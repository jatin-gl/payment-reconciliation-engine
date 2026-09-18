# --- build stage ------------------------------------------------------------
FROM golang:1.23-alpine AS build
WORKDIR /src

# Cache module downloads separately from the source for faster rebuilds.
COPY go.mod ./
RUN go mod download

COPY . .
# CGO disabled -> a fully static binary that runs on scratch/distroless.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# --- runtime stage ----------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
CMD ["--addr", ":8080"]
