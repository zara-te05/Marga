# Imagen de marga-server (el front va embebido en el binario).
#   docker build -t marga-server .
ARG GO_VERSION=1.27

FROM golang:${GO_VERSION}-alpine AS compilar
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/zara-te05/Marga/internal/version.Version=${VERSION} -X github.com/zara-te05/Marga/internal/version.Commit=${COMMIT}" \
      -o /marga-server ./cmd/marga-server

# Sin shell ni paquetes: solo el binario, corriendo como usuario sin privilegios.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=compilar /marga-server /marga-server
ENV MARGA_ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/marga-server"]
