---
title: KeenProxy
hide:
  - navigation
  - toc
---

<div class="kp-landing" markdown>

<section class="kp-hero" markdown>
<div class="kp-hero__text" markdown>

![Логотип KeenProxy](../assets/logo.png){ .kp-hero__logo width="72" }

# Ваша сеть — ваши маршруты

Выборочная маршрутизация трафика для роутеров Keenetic и Netcraze с Entware. Выбирайте, какие устройства, домены, адреса и страны идут через туннель, напрямую или блокируются, — в одной веб-панели.

<div class="kp-actions" markdown>
[:material-play: Открыть демо](../demo/){ .md-button .md-button--primary }
[Начать работу](getting-started.md){ .md-button }
</div>

</div>

[![Обзор веб-панели KeenProxy с синтетическим трафиком](../assets/screenshots/ru-landing-overview.png){ .kp-hero__shot }](../demo/)

</section>

<section class="kp-section" markdown>

## Возможности

<div class="kp-grid" markdown>

<div class="kp-card" markdown>
:material-devices:{ .kp-card__icon }

### Политики для устройств и сетей

Раздельная маршрутизация, полный туннель или прямой доступ для сети и отдельные правила для устройств по MAC-адресу.
</div>

<div class="kp-card" markdown>
:material-earth:{ .kp-card__icon }

### Группы доменов, IP и стран

Маршрутизация по домену, CIDR или стране — в том числе с исключениями вроде «всё, кроме России».
</div>

<div class="kp-card" markdown>
:material-vpn:{ .kp-card__icon }

### Разные типы соединений

WireGuard, ссылки подключения sing-box, outbound-объекты sing-box и необязательный клиент TrustTunnel.
</div>

<div class="kp-card" markdown>
:material-shield-sync:{ .kp-card__icon }

### Резервирование

Переход на другое соединение при отказе и возврат после восстановления.
</div>

<div class="kp-card" markdown>
:material-dns:{ .kp-card__icon }

### DNS и блок-листы

Локальный DNS-фронтенд с резолверами DoH/DoT, выбором маршрута для DNS и блок-листами на уровне DNS.
</div>

<div class="kp-card" markdown>
:material-file-document-check-outline:{ .kp-card__icon }

### Проверка перед применением

Все изменения попадают в общий черновик. Примените его, когда готовы, или отмените; журналы объясняют результат.
</div>

</div>

</section>

<section class="kp-section" markdown>

## Как это работает

<div class="kp-steps" markdown>

<div class="kp-step" markdown>
### Добавьте соединение

Импортируйте файл WireGuard или ссылку подключения либо воспользуйтесь мастером.
</div>

<div class="kp-step" markdown>
### Создайте группы

Соберите домены, адреса или страны и выберите, куда их направить.
</div>

<div class="kp-step" markdown>
### Выберите устройства и примените

Укажите, какие сети и клиенты следуют правилам, и примените черновик.
</div>

</div>

KeenProxy также следит за своими правилами перехвата. На совместимых прошивках Keenetic/Netcraze хук netfilter запускает проверку после изменений firewall; периодический сторож работает везде, включая экспериментальные установки OpenWrt. [Как устроено восстановление правил](network-flow.md#как-восстанавливаются-правила-после-изменений-прошивки).

</section>

<section class="kp-section" markdown>

## Как выглядит панель

<div class="kp-gallery" markdown>

<figure markdown>
[![Группы доменов, маршруты и наблюдаемые адреса](../assets/screenshots/ru-rules.png)](user-guide.md)
<figcaption>Правила</figcaption>
</figure>

<figure markdown>
[![Профили соединений и маршруты](../assets/screenshots/ru-connections.png)](user-guide.md)
<figcaption>Соединения</figcaption>
</figure>

<figure markdown>
[![Диагностика выбранного соединения](../assets/screenshots/ru-diagnostics.png)](user-guide.md)
<figcaption markdown>Диагностика. [Как скачать ZIP на роутере](troubleshooting.md#download-the-diagnostic-zip).</figcaption>
</figure>

</div>

</section>

<section class="kp-cta" markdown>

## Попробуйте в браузере

Демо запускает настоящую веб-панель с синтетическими данными. Роутер и учётные данные не нужны.

<div class="kp-actions" markdown>
[:material-play: Открыть демо](../demo/){ .md-button .md-button--primary }
[Читать обзор](overview.md){ .md-button }
</div>

<p class="kp-note">KeenProxy — экспериментальный проект. Совместимость зависит от архитектуры роутера, прошивки, возможностей ядра и состава сборки.</p>

</section>

</div>
