package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// tempKeychain создаёт временную связку ключей. Настоящая связка и список
// связок пользователя не меняются.
func tempKeychain(t *testing.T) Keychain {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("Связка ключей есть только в macOS")
	}
	if _, err := os.Stat(securityTool); err != nil {
		t.Skip("нет утилиты security")
	}
	path := filepath.Join(t.TempDir(), "test.keychain-db")
	if out, err := exec.Command(securityTool, "create-keychain", "-p", "test", path).CombinedOutput(); err != nil {
		t.Fatalf("временная связка ключей: %v: %s", err, out)
	}
	t.Cleanup(func() { exec.Command(securityTool, "delete-keychain", path).Run() })
	return Keychain{Path: path}
}

func TestKeychainRoundTrip(t *testing.T) {
	kc := tempKeychain(t)
	ctx := context.Background()
	const service = "Daily Agent Test"

	if _, err := kc.Get(ctx, service, KeychainAccount); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("пустая связка: %v", err)
	}
	// Все допустимые знаки: в команде утилиты они идут в кавычках.
	var b strings.Builder
	for r := '!'; r <= '~'; r++ {
		if r != '"' && r != '\\' {
			b.WriteRune(r)
		}
	}
	key := b.String()
	if err := kc.Set(ctx, service, KeychainAccount, key); err != nil {
		t.Fatal(err)
	}
	if got, err := kc.Get(ctx, service, KeychainAccount); err != nil || got != key {
		t.Fatalf("ключ: %q, %v", got, err)
	}
	// Повторная запись заменяет ключ.
	if err := kc.Set(ctx, service, KeychainAccount, "test-key-2"); err != nil {
		t.Fatal(err)
	}
	if got, _ := kc.Get(ctx, service, KeychainAccount); got != "test-key-2" {
		t.Fatalf("после замены: %q", got)
	}
	if err := kc.Delete(ctx, service, KeychainAccount); err != nil {
		t.Fatal(err)
	}
	if _, err := kc.Get(ctx, service, KeychainAccount); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("после удаления: %v", err)
	}
	if err := kc.Delete(ctx, service, KeychainAccount); err != nil {
		t.Fatalf("удаление несуществующей записи: %v", err)
	}
}

func TestKeychainRefusesBadInput(t *testing.T) {
	ctx := context.Background()
	// Нет файла связки — ничего не пишется, даже в связку по умолчанию.
	missing := Keychain{Path: filepath.Join(t.TempDir(), "missing.keychain-db")}
	if err := missing.Set(ctx, "Daily Agent Test", KeychainAccount, "test-key"); err == nil {
		t.Fatal("ждали ошибку без файла связки")
	}
	if _, err := missing.Get(ctx, "Daily Agent Test", KeychainAccount); err == nil || errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("чтение без файла связки: %v", err)
	}

	kc := tempKeychain(t)
	if err := kc.Set(ctx, `Daily "Agent"`, KeychainAccount, "test-key"); err == nil {
		t.Fatal("ждали ошибку на кавычки в имени записи")
	}
	for _, bad := range []string{"", "with space", `quo"te`, `back\slash`, "кириллица", "tab\tkey", strings.Repeat("a", MaxKeyLength+1)} {
		if err := ValidateKey(bad); err == nil {
			t.Errorf("ключ %q прошёл проверку", bad)
		}
		if err := kc.Set(ctx, "Daily Agent Test", KeychainAccount, bad); err == nil {
			t.Errorf("ключ %q записан", bad)
		}
	}
	if err := ValidateKey("sk-test_ABC.123:xyz/+="); err != nil {
		t.Errorf("обычный ключ: %v", err)
	}
}

func TestModelFrom(t *testing.T) {
	kc := tempKeychain(t)
	ctx := context.Background()
	if _, err := ModelFrom(ctx, kc, "", "test-model"); !errors.Is(err, ErrNoModel) {
		t.Fatalf("без адреса: %v", err)
	}
	m, err := ModelFrom(ctx, kc, " http://127.0.0.1:9/v1 ", "test-model")
	if err != nil || m.APIKey != "" || m.BaseURL != "http://127.0.0.1:9/v1" {
		t.Fatalf("без ключа: %+v, %v", m, err)
	}
	if err := kc.Set(ctx, KeychainService, KeychainAccount, "test-key-3"); err != nil {
		t.Fatal(err)
	}
	m, err = ModelFrom(ctx, kc, "http://127.0.0.1:9/v1", "test-model")
	if err != nil || m.APIKey != "test-key-3" {
		t.Fatalf("с ключом: %+v, %v", m, err)
	}
}
