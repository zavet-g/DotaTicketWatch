<div align="center">

![header](https://capsule-render.vercel.app/api?type=waving&color=0:0a0a0a,100:0d1f2d&height=220&section=header&text=DotaTicketWatch&fontColor=f0f0f0&fontSize=54&fontAlign=50&fontAlignY=58&animation=fadeIn&desc=TI%202026%20ticket%20monitor&descAlign=50&descAlignY=78&descSize=16&descFontColor=555555)

[![Typing SVG](https://readme-typing-svg.demolab.com?font=Fira+Code&size=13&duration=2800&pause=900&color=4a4a4a&center=true&vCenter=true&width=480&lines=watching+axs.com+%C2%B7+3+event+sources;watching+steam+news+%C2%B7+valve+api;fires+the+second+something+changes)](https://github.com/artem/dotaticketwatch)

&nbsp;

![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-compose-2496ED?style=flat-square&logo=docker&logoColor=white)
![Telegram](https://img.shields.io/badge/Telegram-bot-26A5E4?style=flat-square&logo=telegram&logoColor=white)
![bbolt](https://img.shields.io/badge/bbolt-embedded-555555?style=flat-square)
![Status](https://img.shields.io/badge/status-watching-brightgreen?style=flat-square)

</div>

&nbsp;

telegram бот, который следит за появлением билетов на TI 2026 и уведомляет в ту же секунду. не раньше, не позже.

построен потому что вручную обновлять страницу каждый час — это не план.

&nbsp;

<div align="center">

[![Typing SVG](https://readme-typing-svg.demolab.com?font=Fira+Code&size=12&duration=1400&pause=500&color=2d2d2d&center=true&vCenter=true&width=540&lines=%5Baxs%5D+performerEventsData+%C2%B7+0+events;%5Baxs%5D+teamUpcoming+%C2%B7+none;%5Bsteam%5D+10+items+%C2%B7+no+match;%C2%B7+all+clear+%C2%B7+27.03+%C2%B7+23%3A14%3A08;%E2%94%80+%E2%94%80+%E2%94%80+%E2%94%80+%E2%94%80+%E2%94%80+%E2%94%80+%E2%94%80+%E2%94%80;%E2%96%B8+%5Baxs%5D+new+event+detected+%C2%B7+5872310;%E2%96%B8+notified+2+subscribers)](https://github.com/artem/dotaticketwatch)

</div>

---

&nbsp;

## как детектируем

```
axs.com ──▸  __NEXT_DATA__ × 3 sources  ──┐
             queue-it waitroom detect      ├──▸  bbolt dedup  ──▸  🚨 telegram
steam ────▸  keyword match  ───────────────┘
```

&nbsp;

два независимых монитора крутятся параллельно с интервалом ≥2 минуты (default 5, джиттер ±25% против cloudflare pattern detection):

&nbsp;

**· axs.com** — AXS рендерит страницу через Next.js SSR. весь стейт лежит в `__NEXT_DATA__` JSON прямо в HTML. парсим три источника внутри:

```
performerEventsData.eventItems[]      — публичные листинги событий
teamUpcomingEventData.upcomingEvent   — предстоящее событие команды
discoveryPerformerData.events[]       — индекс поиска
```

если хоть в одном источнике появился ID, которого нет в базе — алерт.

дополнительный сигнал: Queue-it. когда AXS начинает пускать трафик через очередь (`queueit-overlay`, `inqueue.queue-it.net`) — это значит страница под нагрузкой. билеты живые. детектим и это тоже.

fallback: regex по raw HTML если `__NEXT_DATA__` вдруг исчезнет.

&nbsp;

**· steam news** — Valve анонсирует всё через Steam News API раньше чем открывает продажу. слушаем appid 570 (Dota 2), матчим по ключевым фразам:

```
{"ticket", "international"}
{"ticket", "dota"}
{"on sale", "international"}
```

это самый ранний возможный сигнал — раньше AXS, раньше твиттера.

&nbsp;

оба монитора дедуплицируют через bbolt: ID попал в базу → больше никогда не триггернёт.

---

&nbsp;

## стек и почему

```
Go           — статический бинарь, 15MB docker образ, нет рантайм зависимостей
bbolt        — embedded kv хранилище. без postgres, без redis. просто файл.
tls-client   — имитирует TLS fingerprint Chrome 120 на уровне хэндшейка
FlareSolverr — headless Chrome для JS-челленджей. последний эшелон.
```

&nbsp;

**Go.**
`go build` → статический бинарь → `COPY` в `FROM scratch` → 15MB образ без libc, без python, без ничего. `time.Ticker` + горутины закрывают задачу конкурентного поллинга без async/await и колбэк-ада. stdlib покрывает 90% проекта — `net/http`, `encoding/json`, `log/slog`. сборка за секунду.

**bbolt.**
дедупликация — это задача на принадлежность множеству. bbolt — это embedded B-tree от etcd. ноль инфраструктуры, ноль миграций, данные живут в файле рядом с бинарём. подписчики и seen-события переживают любой рестарт контейнера.

**tls-client.**
стандартный `net/http` шлёт TLS ClientHello который не похож ни на один браузер. Cloudflare ставит Bot Score → block. tls-client патчит fingerprint под Chrome — проблема исчезает до того как запрос дошёл до логики приложения.

**FlareSolverr.**
когда нужен реальный JS — headless Chrome в контейнере решает js-челлендж и отдаёт куки. AXS использует Next.js SSR поэтому обычно не нужен, но присутствует как страховка.

---

&nbsp;

## запуск

```bash
cp .env.example .env
# заполни TELEGRAM_BOT_TOKEN и ADMIN_CHAT_ID

# без flaresolverr:
docker compose up -d

# с flaresolverr:
docker compose --profile flaresolverr up -d
```

---

&nbsp;

## конфиг

| переменная | default | |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | — | обязательно |
| `ADMIN_CHAT_ID` | — | твой chat id, сюда идут ошибки |
| `POLL_INTERVAL_MINUTES` | `5` | минимум 2 |
| `AXS_HUB_URL` | axs.com/teams/1119906/... | хаб TI 2026 |
| `STEAM_NEWS_URL` | steampowered.com API | appid 570 |
| `FLARESOLVERR_URL` | `http://localhost:8191` | опционально |
| `DB_PATH` | `./data/bot.db` | переживает рестарты |

---

&nbsp;

## команды

```
/start    подписаться
/stop     отписаться
/check    проверить вручную          (admin)
/status   статус системы             (admin)
```

admin команды видны только тебе — scoped через `setMyCommands` по chat_id.

---

&nbsp;

## структура

```
cmd/
  bot/          telegram бот + поллинг
  check/        дебаг cli: каскадный фетч + парсинг
internal/
  monitor/      axs + steam news мониторы
  fetcher/      tls-client, flaresolverr, curl fallback
  notifier/     telegram broadcast
  storage/      bbolt: подписчики + seen event ids
  config/       конфиг из env
```

---

&nbsp;

<div align="center">

<sub>Shanghai · Oriental Sports Center · Aug 2026</sub>

&nbsp;

![footer](https://capsule-render.vercel.app/api?type=shark&color=0:0d1f2d,100:0a0a0a&height=60&section=footer)

</div>
