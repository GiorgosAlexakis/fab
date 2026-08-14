# Copyright The FAB Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Image for the internal ontology servers. Both binaries ship in one image and
# the compose file picks which to run, because they are versioned together: the
# object store binds against ontologies the registry serves, and a deployment
# should never mix two releases of that pair.

FROM golang:1.22 AS build

WORKDIR /src

# Dependencies first, so that editing Go files does not re-download the module
# cache on every build.
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY pkg/ pkg/

ARG VERSION=v0.0.0-dev
ARG GIT_COMMIT=unknown
ENV CGO_ENABLED=0
RUN go build \
        -ldflags "-X github.com/GiorgosAlexakis/fab/pkg/version.gitVersion=${VERSION} \
                  -X github.com/GiorgosAlexakis/fab/pkg/version.gitCommit=${GIT_COMMIT}" \
        -o /out/ ./cmd/ontology-registry ./cmd/ontology-objectstore

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/ontology-registry /out/ontology-objectstore /usr/local/bin/

# The registry on 8081, the object store on 8082.
EXPOSE 8081 8082

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/ontology-registry"]
