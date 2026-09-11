# ---- Build stage ----
FROM golang:1.25 AS build
WORKDIR /src

# Cache module downloads first (go.sum must exist — run `go mod tidy` locally).
COPY go.mod go.sum ./
RUN go mod download

# Build a fully static binary so it runs on a distroless/scratch base.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app .

# ---- Run stage ----
# distroless/static: no shell, no package manager, minimal attack surface.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/app /app
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app"]
