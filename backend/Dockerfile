# syntax=docker/dockerfile:1
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/composer ./cmd/composer \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mockprovider ./cmd/mockprovider

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/composer /out/mockprovider /usr/local/bin/
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/composer"]
CMD ["api"]
