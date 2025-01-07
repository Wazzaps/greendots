# build the frontend 
FROM node:22-alpine AS greendots-frontend-builder
WORKDIR /greendots-frontend
COPY \
    greendots-frontend/package.json \
    greendots-frontend/package-lock.json \
    ./
RUN npm install
COPY \
    greendots-frontend/env.d.ts \
    greendots-frontend/index.html \
    greendots-frontend/tsconfig.json \
    greendots-frontend/tsconfig.app.json \
    greendots-frontend/tsconfig.node.json \
    greendots-frontend/vite.config.ts \
    ./
COPY greendots-frontend/src ./src
COPY greendots-frontend/public ./public
RUN node_modules/.bin/vite build

# build the server
FROM golang:1.22.5-alpine AS greendots-server-builder
RUN apk add minify

COPY \
    greendots-server/go.mod \
    greendots-server/go.sum \
    greendots-server/
RUN cd greendots-server && go mod download
COPY \
    greendots-server/api-docs.txt \
    greendots-server/copy_frontend_dist.sh \
    greendots-server/gen_commit_info.sh \
    greendots-server/gen_logs_view.sh \
    greendots-server/logs_view.html \
    greendots-server/main.go \
    greendots-server/tail_logs_view_prefix.html \
    greendots-server/
COPY --from=greendots-frontend-builder /greendots-frontend/dist/ greendots-frontend/dist/
# Copy enough of the git repo to make `buildvcs` work
RUN mkdir -p .git/objects
COPY .git/HEAD .git/HEAD
COPY .git/refs .git/refs
RUN cd greendots-server && go generate . && go build -trimpath .

# and now create the final runtime image
FROM alpine:latest
COPY --from=greendots-server-builder /go/greendots-server/greendots /usr/local/bin/greendots
