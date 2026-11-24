# Daily Agent

Личный рабочий помощник для macOS: повестка дня, встречи, задачи, чат с агентом.

Архитектура и план по шагам — в [`docs/architecture.md`](docs/architecture.md), макеты экранов — в [`docs/mockups/`](docs/mockups/). Сейчас готов шаг 1: окно, боковая панель, локальная база и экран «Сегодня» на тестовых данных.

## Что нужно

| Инструмент | Версия |
| -- | -- |
| macOS | 13 или новее |
| Go | 1.26 или новее. В `go.mod` закреплён `toolchain go1.27.4` |
| Node и npm | Node 20.19 или новее |
| Инструменты командной строки Xcode | `xcode-select --install` |
| Wails CLI | v3.1.0 |

Wails CLI ставится так:

```bash
GO111MODULE=on go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

Команда кладёт `wails3` в `~/go/bin`. Эта папка должна быть в `PATH`. Проверка окружения:

```bash
wails3 doctor
```

## Запуск в режиме разработки

```bash
wails3 dev
```

Команда ставит пакеты интерфейса, создаёт привязки TypeScript к сервисам на Go, собирает приложение и открывает окно. Правки интерфейса подхватываются сразу, правки Go — после пересборки.

## Тестовые данные

Данные вымышленные и похожи на макет «Сегодня». Даты считаются от текущего дня.

```bash
wails3 task db:seed          # наполнить пустую базу
wails3 task db:reset YES=1   # удалить все записи, схема останется
```

Обе команды работают с базой приложения. Другую базу можно указать через `DB=путь`, например `wails3 task db:seed DB=/tmp/dev.db`. Те же действия без Wails:

```bash
go run ./cmd/devdata seed [-db путь]
go run ./cmd/devdata reset -yes [-db путь]
```

`seed` отказывается работать с непустой базой, а `reset` — без подтверждения. Команды печатают только число записей.

## Где лежат данные

База — один файл SQLite: `~/Library/Application Support/Daily Agent/daily-agent.db`. Схема создаётся встроенными миграциями при запуске. Переменная окружения `DAILY_AGENT_DB` задаёт другой путь и действует и на приложение, и на команды выше.

## Сборка

```bash
wails3 build     # программа bin/daily-agent
wails3 package   # пакет bin/daily-agent.app с локальной подписью
```

## Тесты

```bash
go test ./...
```

## Структура

| Папка | Что в ней |
| -- | -- |
| `main.go` | Запуск приложения, окно, подключение сервисов |
| `internal/store` | База, миграции (`migrations/*.sql`), запись и чтение |
| `internal/app` | Сервисы, которые вызывает окно: «Сегодня» и боковая панель |
| `internal/devdata` | Тестовые данные и очистка базы |
| `cmd/devdata` | Команда `seed` / `reset` |
| `frontend/` | Интерфейс на React и TypeScript; `src/styles/tokens.css` — цвета, отступы и размеры |
| `frontend/bindings/` | Привязки к сервисам. Создаются командой `wails3 generate bindings`, вручную не правятся |
| `build/` | Настройки сборки и пакетов Wails |
| `docs/` | Архитектура и макеты |

## Закреплённые версии

| Что | Версия |
| -- | -- |
| Wails | `github.com/wailsapp/wails/v3 v3.1.0`, `@wailsio/runtime 3.1.0` |
| SQLite | `modernc.org/sqlite v1.63.0` |
| UUID | `github.com/google/uuid v1.6.0` |
| React | `react 18.3.1`, `react-dom 18.3.1` |
| Сборка интерфейса | `vite 8.3.8`, `@vitejs/plugin-react 6.3.0`, `typescript 5.9.3` |

Версии в `frontend/package.json` указаны без диапазонов, а `.npmrc` с `save-exact=true` сохраняет новые пакеты так же.
