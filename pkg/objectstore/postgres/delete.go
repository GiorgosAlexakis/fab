/*
Copyright The FAB Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
)

// deleteObjects removes the given objects and whatever their link types say
// should go with them.
//
// It resolves the whole set before deleting anything, so that a restrict policy
// anywhere in the closure aborts the delete rather than leaving half of it
// applied. Property rows, unique-value claims and link rows are removed by the
// foreign keys, which is why only the object rows are deleted here.
func (s *Store) deleteObjects(ctx context.Context, tx pgx.Tx, roots []int64) error {
	doomed := make(map[int64]bool, len(roots))
	order := make([]int64, 0, len(roots))
	pending := append([]int64(nil), roots...)

	for len(pending) > 0 {
		objectID := pending[0]
		pending = pending[1:]
		if doomed[objectID] {
			continue
		}
		doomed[objectID] = true
		order = append(order, objectID)

		typeID, err := objectTypeID(ctx, tx, objectID)
		if errors.Is(err, objectstore.ErrNotFound) {
			// Something else deleted it, which is the outcome we wanted.
			continue
		}
		if err != nil {
			return err
		}

		for _, linkType := range s.binding.OutgoingLinks(typeID) {
			cascaded, err := s.applyDeletePolicy(ctx, tx, linkType, objectID)
			if err != nil {
				return err
			}
			pending = append(pending, cascaded...)
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM objects WHERE object_id = ANY($1)`, order); err != nil {
		return fmt.Errorf("deleting objects: %w", err)
	}
	return nil
}

// applyDeletePolicy returns the objects that must be deleted along with
// objectID because of one of its outgoing link types, and fails when the policy
// forbids the delete.
func (s *Store) applyDeletePolicy(
	ctx context.Context,
	tx pgx.Tx,
	linkType objectstore.LinkType,
	objectID int64,
) ([]int64, error) {
	switch ontologyv1.DeletePolicy(linkType.OnSourceDelete) {
	case ontologyv1.DeletePolicyCascade:
		return linkedTargets(ctx, tx, linkType.ID, objectID)

	case ontologyv1.DeletePolicyDetach, ontologyv1.DeletePolicySetNull:
		// Both mean "the objects on the other side survive". A link is a row of
		// its own rather than a column on the target, so there is no reference
		// to null out: dropping the link row is the whole of it, and the
		// foreign key does that when the object row goes.
		return nil, nil

	default:
		// restrict, and anything unrecognised: never destroy data implicitly.
		blocking, found, err := oneLinkedTarget(ctx, tx, linkType.ID, objectID)
		if err != nil {
			return nil, err
		}
		if found {
			return nil, fmt.Errorf("%s still links %s to %s and its delete policy is %s; unlink it first: %w",
				linkType.QualifiedName, s.describe(ctx, tx, objectID),
				s.refFor(linkType.TargetTypeID, blocking),
				ontologyv1.DeletePolicyRestrict, objectstore.ErrLinked)
		}
		return nil, nil
	}
}

func objectTypeID(ctx context.Context, tx pgx.Tx, objectID int64) (int32, error) {
	var typeID int32
	err := tx.QueryRow(ctx,
		`SELECT object_type_id FROM objects WHERE object_id = $1 FOR UPDATE`, objectID).Scan(&typeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("object %d: %w", objectID, objectstore.ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("reading object %d: %w", objectID, err)
	}
	return typeID, nil
}

func linkedTargets(ctx context.Context, tx pgx.Tx, linkID int32, sourceID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `
		SELECT target_object_id FROM object_links
		WHERE link_id = $1 AND source_object_id = $2`, linkID, sourceID)
	if err != nil {
		return nil, fmt.Errorf("reading the links of object %d: %w", sourceID, err)
	}
	defer rows.Close()

	var targets []int64
	for rows.Next() {
		var targetID int64
		if err := rows.Scan(&targetID); err != nil {
			return nil, fmt.Errorf("reading the links of object %d: %w", sourceID, err)
		}
		targets = append(targets, targetID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading the links of object %d: %w", sourceID, err)
	}
	return targets, nil
}

// oneLinkedTarget names a single object on the far end of a link, for an error
// message that says what is in the way.
func oneLinkedTarget(ctx context.Context, tx pgx.Tx, linkID int32, sourceID int64) (string, bool, error) {
	var primaryKey string
	err := tx.QueryRow(ctx, `
		SELECT o.primary_key
		FROM object_links l
		JOIN objects o ON o.object_id = l.target_object_id
		WHERE l.link_id = $1 AND l.source_object_id = $2
		LIMIT 1`, linkID, sourceID).Scan(&primaryKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading the links of object %d: %w", sourceID, err)
	}
	return primaryKey, true, nil
}
