# Atajos para Linux/macOS (en Windows usa los comandos de go directamente).
.PHONY: compilar probar probar-todo css servidor bd

compilar:            ## compila marga y marga-server en bin/
	go build -o bin/ ./cmd/...

probar:              ## pruebas unitarias (sin base de datos)
	go test ./...

probar-todo:         ## también las de integración: necesita PostgreSQL (make bd)
	MARGA_TEST_DB_URL=$${MARGA_TEST_DB_URL:-postgres://marga:marga@localhost:5432/marga?sslmode=disable} go test -count=1 ./...

css:                 ## regenera web/assets/css/marga.css
	cd web && npm install --no-audit --no-fund && npm run css

servidor:            ## servidor con datos demo y el front leído de disco
	MARGA_WEB_DIR=./web go run ./cmd/marga-server -seed

bd:                  ## PostgreSQL de desarrollo en Docker
	docker compose -f docker-compose.dev.yml up -d
