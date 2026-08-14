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
	"github.com/GiorgosAlexakis/fab/pkg/storage"
)

// linkEnds are the two objects a link connects, once both refs have been
// resolved. The refs are carried alongside the ids so that an error can name the
// objects the way the caller did.
type linkEnds struct {
	linkType objectstore.LinkType
	source   objectstore.Ref
	target   objectstore.Ref
	sourceID int64
	targetID int64
}

// Link connects two objects.
//
// Linking an already linked pair is a no-op, so that a caller replaying a
// message does not have to check first.
func (s *Store) Link(ctx context.Context, request objectstore.LinkRequest) error {
	linkType, err := s.linkTypeFor(request)
	if err != nil {
		return err
	}

	return storage.InTx(ctx, s.db, func(tx pgx.Tx) error {
		ends, err := s.resolveLinkEnds(ctx, tx, linkType, request)
		if err != nil {
			return err
		}
		if err := s.enforceCardinality(ctx, tx, ends); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO object_links (link_id, source_object_id, target_object_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (link_id, source_object_id, target_object_id) DO NOTHING`,
			linkType.ID, ends.sourceID, ends.targetID); err != nil {
			return fmt.Errorf("linking %s to %s over %s: %w",
				request.Source, request.Target, linkType.QualifiedName, err)
		}
		return nil
	})
}

// Unlink disconnects two objects. Unlinking a pair that is not linked is a
// no-op.
func (s *Store) Unlink(ctx context.Context, request objectstore.LinkRequest) error {
	linkType, err := s.linkTypeFor(request)
	if err != nil {
		return err
	}

	return storage.InTx(ctx, s.db, func(tx pgx.Tx) error {
		ends, err := s.resolveLinkEnds(ctx, tx, linkType, request)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM object_links
			WHERE link_id = $1 AND source_object_id = $2 AND target_object_id = $3`,
			linkType.ID, ends.sourceID, ends.targetID); err != nil {
			return fmt.Errorf("unlinking %s from %s over %s: %w",
				request.Source, request.Target, linkType.QualifiedName, err)
		}
		return nil
	})
}

// Traverse returns the objects reachable from one object along a traversal.
func (s *Store) Traverse(
	ctx context.Context,
	request objectstore.TraverseRequest,
) ([]objectstore.Object, error) {
	fromType, err := s.binding.ObjectType(request.From.Type)
	if err != nil {
		return nil, err
	}
	traversal, err := s.binding.Traversal(request.From.Type, request.Traversal)
	if err != nil {
		return nil, err
	}
	toType, err := s.binding.ObjectTypeByID(traversal.ToTypeID)
	if err != nil {
		return nil, err
	}

	limit, err := pageSize(request.Limit)
	if err != nil {
		return nil, err
	}
	if request.Offset < 0 {
		return nil, fmt.Errorf("offset %d is negative", request.Offset)
	}

	fromID, err := resolveObjectID(ctx, s.db, fromType, request.From.PrimaryKey)
	if err != nil {
		return nil, err
	}

	// One link row serves both directions; which column is the anchor and which
	// is the result is the only difference between them.
	anchor, reached := "source_object_id", "target_object_id"
	if !traversal.Forward {
		anchor, reached = reached, anchor
	}

	rows, err := s.db.Query(ctx, fmt.Sprintf(`
		SELECT o.object_id, o.primary_key, o.created_at, o.updated_at
		FROM object_links l
		JOIN objects o ON o.object_id = l.%s
		WHERE l.link_id = $1 AND l.%s = $2
		ORDER BY o.object_id
		LIMIT $3 OFFSET $4`, reached, anchor),
		traversal.Link.ID, fromID, limit, request.Offset)
	if err != nil {
		return nil, fmt.Errorf("traversing %s from %s: %w", request.Traversal, request.From, err)
	}
	objectRows, err := scanObjectRows(rows, toType.ID)
	if err != nil {
		return nil, fmt.Errorf("traversing %s from %s: %w", request.Traversal, request.From, err)
	}

	return s.hydrate(ctx, toType, objectRows)
}

// linkTypeFor resolves the link type of a request and checks that the two refs
// are of the types the link connects. A link is typed, so an object on the wrong
// end is a caller bug rather than a missing row.
func (s *Store) linkTypeFor(request objectstore.LinkRequest) (objectstore.LinkType, error) {
	linkType, err := s.binding.LinkType(request.Link)
	if err != nil {
		return objectstore.LinkType{}, err
	}

	sourceType, err := s.binding.ObjectType(request.Source.Type)
	if err != nil {
		return objectstore.LinkType{}, err
	}
	if sourceType.ID != linkType.SourceTypeID {
		expected, err := s.binding.ObjectTypeByID(linkType.SourceTypeID)
		if err != nil {
			return objectstore.LinkType{}, err
		}
		return objectstore.LinkType{}, fmt.Errorf("%s links from %s, not from %s: %w",
			linkType.QualifiedName, expected.QualifiedName, sourceType.QualifiedName,
			objectstore.ErrTypeMismatch)
	}

	targetType, err := s.binding.ObjectType(request.Target.Type)
	if err != nil {
		return objectstore.LinkType{}, err
	}
	if targetType.ID != linkType.TargetTypeID {
		expected, err := s.binding.ObjectTypeByID(linkType.TargetTypeID)
		if err != nil {
			return objectstore.LinkType{}, err
		}
		return objectstore.LinkType{}, fmt.Errorf("%s links to %s, not to %s: %w",
			linkType.QualifiedName, expected.QualifiedName, targetType.QualifiedName,
			objectstore.ErrTypeMismatch)
	}

	return linkType, nil
}

// resolveLinkEnds turns the two refs into object ids and locks both rows, so
// that a concurrent write cannot delete an endpoint or add a competing link
// between the cardinality check and the insert.
func (s *Store) resolveLinkEnds(
	ctx context.Context,
	tx pgx.Tx,
	linkType objectstore.LinkType,
	request objectstore.LinkRequest,
) (linkEnds, error) {
	sourceType, err := s.binding.ObjectTypeByID(linkType.SourceTypeID)
	if err != nil {
		return linkEnds{}, err
	}
	targetType, err := s.binding.ObjectTypeByID(linkType.TargetTypeID)
	if err != nil {
		return linkEnds{}, err
	}

	source := objectstore.Ref{Type: sourceType.QualifiedName, PrimaryKey: request.Source.PrimaryKey}
	target := objectstore.Ref{Type: targetType.QualifiedName, PrimaryKey: request.Target.PrimaryKey}

	sourceID, err := resolveObjectID(ctx, tx, sourceType, source.PrimaryKey)
	if err != nil {
		return linkEnds{}, err
	}
	targetID, err := resolveObjectID(ctx, tx, targetType, target.PrimaryKey)
	if err != nil {
		return linkEnds{}, err
	}

	// Lock in object id order: two callers linking the same pair from opposite
	// ends would otherwise deadlock.
	if _, err := tx.Exec(ctx, `
		SELECT object_id FROM objects
		WHERE object_id = ANY($1)
		ORDER BY object_id
		FOR UPDATE`, []int64{sourceID, targetID}); err != nil {
		return linkEnds{}, fmt.Errorf("locking the endpoints of %s: %w", linkType.QualifiedName, err)
	}

	return linkEnds{
		linkType: linkType,
		source:   source,
		target:   target,
		sourceID: sourceID,
		targetID: targetID,
	}, nil
}

// enforceCardinality rejects a link that would give an object more neighbours
// than its link type allows. The check excludes the pair being linked, so
// re-linking an existing pair stays a no-op.
func (s *Store) enforceCardinality(ctx context.Context, tx pgx.Tx, ends linkEnds) error {
	cardinality := ontologyv1.Cardinality(ends.linkType.Cardinality)

	// A target may have at most one source unless many sources are allowed.
	singleSource := cardinality == ontologyv1.CardinalityOneToOne ||
		cardinality == ontologyv1.CardinalityOneToMany
	// A source may have at most one target unless many targets are allowed.
	singleTarget := cardinality == ontologyv1.CardinalityOneToOne ||
		cardinality == ontologyv1.CardinalityManyToOne

	if singleTarget {
		existing, found, err := otherEnd(ctx, tx, ends, true)
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("%s is %s: %s is already linked to %s: %w",
				ends.linkType.QualifiedName, ends.linkType.Cardinality,
				ends.source, s.refFor(ends.linkType.TargetTypeID, existing),
				objectstore.ErrCardinality)
		}
	}
	if singleSource {
		existing, found, err := otherEnd(ctx, tx, ends, false)
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("%s is %s: %s is already linked from %s: %w",
				ends.linkType.QualifiedName, ends.linkType.Cardinality,
				ends.target, s.refFor(ends.linkType.SourceTypeID, existing),
				objectstore.ErrCardinality)
		}
	}
	return nil
}

// otherEnd reports whether one end of a link already has a neighbour other than
// the one being linked, naming it so the error can say what is in the way.
func otherEnd(ctx context.Context, tx pgx.Tx, ends linkEnds, fromSource bool) (string, bool, error) {
	anchor, other := "source_object_id", "target_object_id"
	anchorID, otherID := ends.sourceID, ends.targetID
	if !fromSource {
		anchor, other = other, anchor
		anchorID, otherID = otherID, anchorID
	}

	var primaryKey string
	err := tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT o.primary_key
		FROM object_links l
		JOIN objects o ON o.object_id = l.%s
		WHERE l.link_id = $1 AND l.%s = $2 AND l.%s <> $3
		LIMIT 1`, other, anchor, other),
		ends.linkType.ID, anchorID, otherID).Scan(&primaryKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("checking the cardinality of %s: %w", ends.linkType.QualifiedName, err)
	}
	return primaryKey, true, nil
}

// refFor renders an object of a known type as "type/primaryKey" for error
// messages, falling back to the primary key alone when the type is not one this
// binding covers.
func (s *Store) refFor(typeID int32, primaryKey string) string {
	objectType, err := s.binding.ObjectTypeByID(typeID)
	if err != nil {
		return primaryKey
	}
	return objectstore.Ref{Type: objectType.QualifiedName, PrimaryKey: primaryKey}.String()
}

// describe renders an object identified only by its id, which is the shape a
// delete walking a link closure has. It is an error path, so the extra lookup is
// worth the message being readable.
func (s *Store) describe(ctx context.Context, db storage.DB, objectID int64) string {
	var (
		typeID     int32
		primaryKey string
	)
	if err := db.QueryRow(ctx,
		`SELECT object_type_id, primary_key FROM objects WHERE object_id = $1`, objectID).
		Scan(&typeID, &primaryKey); err != nil {
		return fmt.Sprintf("object %d", objectID)
	}
	return s.refFor(typeID, primaryKey)
}

// resolveObjectID looks up the store-internal id of a ref.
func resolveObjectID(
	ctx context.Context,
	db storage.DB,
	objectType *objectstore.ObjectType,
	primaryKey string,
) (int64, error) {
	var objectID int64
	err := db.QueryRow(ctx, `
		SELECT object_id FROM objects
		WHERE object_type_id = $1 AND primary_key = $2`, objectType.ID, primaryKey).Scan(&objectID)
	ref := objectstore.Ref{Type: objectType.QualifiedName, PrimaryKey: primaryKey}
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("%s: %w", ref, objectstore.ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("reading object %s: %w", ref, err)
	}
	return objectID, nil
}
