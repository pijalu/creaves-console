# Multi-stage build (mirrors creaves/Dockerfile, minus the npm/yarn asset
# pipeline — creaves-console has no webpack assets, templates and public
# files are embedded via go:embed).
FROM golang AS builder

ENV GOPROXY http://proxy.golang.org
RUN go install github.com/gobuffalo/cli/cmd/buffalo@latest
RUN mkdir -p /src/creaves-console
WORKDIR /src/creaves-console

# Copy the Go Modules manifests and cache deps before copying source so
# that source changes don't invalidate the downloaded layer.
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

ADD . .
RUN buffalo plugins install
RUN buffalo build --environment production --static -o /bin/app

FROM alpine

ARG TZ='Europe/Brussels'
ENV DEFAULT_TZ ${TZ}

RUN apk add --no-cache bash ca-certificates tzdata \
  && cp /usr/share/zoneinfo/${DEFAULT_TZ} /etc/localtime

WORKDIR /bin/

COPY --from=builder /bin/app .
COPY dockerscript/* /bin/

ENV GO_ENV production

# Bind the app to 0.0.0.0 so it can be seen from outside the container
ENV ADDR 0.0.0.0

EXPOSE 3001

# Migrate + seed, then start the web server (same pattern as creaves).
CMD /bin/quickstart.prod.sh
