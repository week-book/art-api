# syntax=docker/dockerfile:1

# --- Builder ---
# Go 1.22 закреплён под версию в go.mod; не bump без явного решения —
# pgx/v5 на новых минорных версиях Go требовал toolchain 1.25+ при
# разработке этого Dockerfile (см. комментарий про GOTOOLCHAIN ниже).
FROM golang:1.22-bookworm AS builder

WORKDIR /src

# Кэшируем слой зависимостей отдельно от исходников — go.mod/go.sum меняются
# реже, чем код, поэтому docker build не должен перекачивать модули на
# каждое изменение .go файла.
COPY go.mod go.sum ./
# GOTOOLCHAIN=local — без этого `go` может попытаться скачать новый toolchain
# (см. go.mod requires новее go 1.22), что недопустимо в офлайн/закрытой
# сборочной среде. Закреплённая версия golang:1.22-bookworm — то, что есть,
# и то, что используется.
ENV GOTOOLCHAIN=local
RUN go mod download

COPY . .

# CGO_ENABLED=0 — статический бинарник, чтобы runtime-стадия могла быть
# distroless/scratch без libc-зависимостей (pgx — чистый Go, без cgo, так что
# это не накладывает ограничений на функциональность).
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/art-api ./cmd/art-api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/artkeys ./cmd/artkeys

# --- Runtime ---
# distroless вместо alpine: нет шелла, нет package manager — меньше
# поверхности атаки для сервиса, который скоро будет смотреть в публичный
# ingress (см. roadmap.md "Infra Readiness" — Deploy трек идёт параллельно
# с Auth в ART-SPRINT-07). artkeys запускается вручную через `docker compose
# run`, не как постоянный сервис — отсутствие шелла в runtime-образе ему не
# мешает.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/art-api ./art-api
COPY --from=builder /out/artkeys ./artkeys
# data/artworks.json — не запекаем в образ: путь монтируется volume'ом
# (см. docker-compose.yml), потому что artworks.json обновляется через
# sheets-export независимо от релизов кода (см. tools.md), и запекание
# в образ означало бы пересборку образа на каждое добавление работы в архив.

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["./art-api"]
CMD ["-addr=:8080", "-data=data/artworks.json"]
