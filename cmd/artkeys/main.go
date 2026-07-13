// Command artkeys — CLI для ручного управления API-ключами art-api.
//
// Сознательно НЕ HTTP-эндпоинт: выдача ключей — это решение Данила "кому
// дать доступ", не самообслуживание. Публичного /keys эндпоинта в art-api
// нет и не планируется (расходится с более ранним планом "свободная
// самовыдача без подтверждения" из architecture.md — это решение от
// 29.06.2026 уточняет его).
//
// Подключается к той же базе, что и сам art-api (DATABASE_URL), и работает
// напрямую с internal/auth — переиспользует генерацию ключа и хеширование,
// чтобы не дублировать эту логику между CLI и middleware.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/week-book/apikeys"
)

const keyPrefix = "wa_live_"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "error: DATABASE_URL is not set")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := apikeys.NewPostgresStore(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: connect to database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	switch os.Args[1] {
	case "create":
		runCreate(ctx, store, os.Args[2:])
	case "list":
		runList(ctx, store)
	case "revoke":
		runRevoke(ctx, store, os.Args[2:])
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `artkeys — управление API-ключами art-api

Использование:
  artkeys create --label "<описание>"   Выдать новый ключ
  artkeys list                          Показать все ключи (включая отозванные)
  artkeys revoke <id>                   Отозвать ключ по id

Требует переменную окружения DATABASE_URL.`)
}

func runCreate(ctx context.Context, store apikeys.Store, args []string) {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	label := fs.String("label", "", "описание ключа, например имя потребителя (обязательно)")
	_ = fs.Parse(args)

	if *label == "" {
		fmt.Fprintln(os.Stderr, "error: --label обязателен (например: --label \"MCP server\")")
		os.Exit(1)
	}

	rawKey, err := apikeys.GenerateKey(keyPrefix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: generate key: %v\n", err)
		os.Exit(1)
	}

	k, err := store.Create(ctx, apikeys.Hash(rawKey), *label)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: save key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Ключ создан (id=%s, label=%q).\n", k.ID, k.Label)
	fmt.Println("Сохрани его — он больше никогда не будет показан:")
	fmt.Println()
	fmt.Println(rawKey)
}

func runList(ctx context.Context, store apikeys.Store) {
	keys, err := store.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: list keys: %v\n", err)
		os.Exit(1)
	}

	if len(keys) == 0 {
		fmt.Println("Ключей пока нет.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tLABEL\tCREATED_AT\tLAST_USED_AT\tREVOKED")
	for _, k := range keys {
		lastUsed := "—"
		if k.LastUsedAt != nil {
			lastUsed = k.LastUsedAt.Format(time.RFC3339)
		}
		revoked := "no"
		if k.RevokedAt != nil {
			revoked = k.RevokedAt.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			k.ID, k.Label, k.CreatedAt.Format(time.RFC3339), lastUsed, revoked)
	}
	w.Flush()
}

func runRevoke(ctx context.Context, store apikeys.Store, args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "error: usage: artkeys revoke <id>")
		os.Exit(1)
	}
	id := args[0]

	if err := store.Revoke(ctx, id); err != nil {
		fmt.Fprintf(os.Stderr, "error: revoke key %s: %v\n", id, err)
		os.Exit(1)
	}
	fmt.Printf("Ключ %s отозван.\n", id)
}
