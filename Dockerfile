# build the frontend 
FROM node:22-alpine as greendots-frontend-builder
COPY greendots-frontend .
RUN npm install
RUN node_modules/.bin/vite build

# build the server
FROM golang:1.22.5-alpine as greendots-server-builder
COPY --from=greendots-frontend-builder dist/ greendots-frontend/dist/
COPY greendots-server greendots-server
RUN apk add minify
RUN cd greendots-server && go generate . && go build -trimpath -buildvcs=true .
RUN cd greendots-server && ls && pwd

# and now create the final runtime image
FROM alpine:latest
COPY --from=greendots-server-builder /go/greendots-server/greendots greendots
