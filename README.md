<div align="center">

<img src="assets/banner.svg" width="100%"/>

[![Typing](https://readme-typing-svg.demolab.com?font=Anton&size=26&duration=2200&pause=900&color=FFFFFF&background=00000000&center=true&vCenter=true&width=900&height=55&lines=WATCHING+BEFORE+ANYONE+ELSE;AXS+%C2%B7+STEAM+%C2%B7+TELEGRAM;TI+2026+%C2%B7+SHANGHAI+%C2%B7+AUG)](https://github.com/zavet-g/DotaTicketWatch)

&nbsp;

![](https://img.shields.io/badge/MADE%20IN-GO-ffffff?style=for-the-badge&color=000000&labelColor=000000&logo=go&logoColor=ffffff)
![](https://img.shields.io/badge/ZERO-DEPS-ffffff?style=for-the-badge&color=000000&labelColor=000000)
![](https://img.shields.io/badge/ALWAYS-WATCHING-ffffff?style=for-the-badge&color=000000&labelColor=000000)
![](https://img.shields.io/badge/TI-2026-ffffff?style=for-the-badge&color=000000&labelColor=000000)

</div>

&nbsp;

пока все обновляют страницу руками — этот уже отправил уведомление.

> **не спит. не устаёт. не пропускает.**

написан на go — компилируется за секунду, **весит 15mb, летит быстрее чем ты успел подумать.**

&nbsp;

<img src="assets/terminal.svg" width="100%"/>

---

&nbsp;

<sub>три источника. один файл. ноль пропусков.</sub>

## С И Г Н А Л Ы

<img src="assets/pipeline.svg" width="100%"/>

&nbsp;

**· axs.com** — AXS рендерит страницу через Next.js SSR. весь стейт лежит в `__NEXT_DATA__` JSON прямо в HTML. парсим три источника внутри:

```
performerEventsData.eventItems[]      — публичные листинги событий
teamUpcomingEventData.upcomingEvent   — предстоящее событие команды
discoveryPerformerData.events[]       — индекс поиска
```

если хоть в одном источнике появился ID, которого нет в базе — **алерт.**

дополнительный сигнал: Queue-it. когда AXS начинает пускать трафик через очередь (`queueit-overlay`, `inqueue.queue-it.net`) — это значит **страница под нагрузкой. билеты живые.** детектим и это тоже.

fallback: regex по raw HTML если `__NEXT_DATA__` вдруг исчезнет.

&nbsp;

**· steam news** — Valve анонсирует всё через Steam News API раньше чем открывает продажу. слушаем appid 570 (Dota 2), матчим по ключевым фразам:

```
{"ticket", "international"}
{"ticket", "dota"}
{"on sale", "international"}
```

&nbsp;

***самый ранний возможный сигнал.***
***раньше axs. раньше твиттера.***

&nbsp;

оба монитора дедуплицируют через bbolt: **ID попал в базу → больше никогда не триггернёт.**

---

&nbsp;

<sub>каждый выбор — под конкретное ограничение.</sub>

## П О Ч Е М У  Т А К

```
Go           — статический бинарь, 15MB docker образ, нет рантайм зависимостей
bbolt        — embedded kv хранилище. без postgres, без redis. просто файл.
tls-client   — имитирует TLS fingerprint Chrome 120 на уровне хэндшейка
FlareSolverr — headless Chrome для JS-челленджей. последний эшелон.
```

&nbsp;

<kbd>Go</kbd>
`go build` → статический бинарь → `COPY` в `FROM scratch` → 15MB образ без libc, без python, без ничего. `time.Ticker` + горутины закрывают задачу конкурентного поллинга без async/await и колбэк-ада. stdlib покрывает 90% проекта. сборка за секунду.

<kbd>bbolt</kbd>
дедупликация — это задача на принадлежность множеству. bbolt — embedded B-tree от etcd. **ноль инфраструктуры, ноль миграций,** данные живут в файле рядом с бинарём. переживает любой рестарт.

<kbd>tls-client</kbd>
стандартный `net/http` шлёт TLS ClientHello который не похож ни на один браузер. Cloudflare ставит Bot Score → block. tls-client патчит fingerprint под Chrome 120 — **проблема исчезает до того как запрос дошёл до логики приложения.**

<kbd>FlareSolverr</kbd>
headless Chrome для JS-челленджей. AXS использует Next.js SSR поэтому обычно не нужен. присутствует как страховка.

---

&nbsp;

## З А П У С К

```bash
cp .env.example .env
# заполни TELEGRAM_BOT_TOKEN и ADMIN_CHAT_ID

docker compose up -d

# с flaresolverr:
docker compose --profile flaresolverr up -d
```

&nbsp;

| переменная | default | |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | — | обязательно |
| `ADMIN_CHAT_ID` | — | твой chat id, сюда идут ошибки |
| `POLL_INTERVAL_MINUTES` | `5` | минимум 2 |
| `AXS_HUB_URL` | axs.com/teams/1119906/... | хаб TI 2026 |
| `STEAM_NEWS_URL` | steampowered.com API | appid 570 |
| `FLARESOLVERR_URL` | `http://localhost:8191` | опционально |
| `DB_PATH` | `./data/bot.db` | переживает рестарты |

&nbsp;

```
/start    подписаться
/stop     отписаться
/check    проверить вручную          (admin)
/status   статус системы             (admin)
```

admin команды видны только тебе — scoped через `setMyCommands` по chat_id.

---

&nbsp;

<img src="assets/tree.svg" width="100%"/>

---

&nbsp;

<div align="center">

<sub>Shanghai · Oriental Sports Center · Aug 2026</sub>

&nbsp;

![footer](https://capsule-render.vercel.app/api?type=venom&color=000000&height=120&section=footer)

</div>
