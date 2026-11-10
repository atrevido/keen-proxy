# AmneziaWG 3.1

Первый выпуск рассчитан на ARM64 и проксирует прикладной TCP. Поддерживается нативный конфигурационный файл AmneziaWG 3.1, включая экспорт Amnezia Premium. Проверьте профиль у своего провайдера перед переключением рабочего маршрута. Сам туннель AWG использует UDP. Прикладной UDP и изменение политики DNS в эту функцию не входят.

Соберите клиент командой `make build-awg-arm64`. Результат находится в `dist/keenproxy-awg-linux-arm64`.

## Keenetic и Netcraze с Entware

Установите дополнительный бинарник ARM64 в `/opt/bin/keenproxy-awg` с правом выполнения. В KeenProxy выберите **Новое соединение → AmneziaWG** и загрузите нативный `.conf`. Перед добавлением в общий черновик нажмите **Проверить HTTPS через AmneziaWG**. Выберите профиль в тестовом правиле маршрутизации и примените черновик.

Первое включение и последующие изменения маршрутов сохраняются до следующего планового запуска службы. Во время подготовки черновика существующие соединения WireGuard и VLESS продолжают работу. Если заменить только файл профиля, сохранив соединение и SOCKS-порт, перезапустится только AWG. Проверка изменённого файла с идентификатором уже активного пира одновременно с его работой недоступна. Текущий профиль можно проверить через уже запущенный SOCKS.

Ожидающий применения черновик сохраняется рядом с активным YAML как `config.yaml.pending-awg` с ограниченными правами. Перед плановым перезапуском сохраните копию активного файла. Для отмены черновика загрузите в панели копию активного YAML и примените её повторно. Если ожидающая конфигурация не запустится, демон попытается восстановить активную.

## MikroTik RouterOS

Маршрут использует RouterOS Container и Socksify. Создайте выделенный транзитный интерфейс с изолированной подсетью IPv4. Разрешите роутеру доступ к SOCKS-порту контейнера с транзитного адреса. Не открывайте этот порт в WAN и обычную LAN. Проверьте наличие пакета Container нужной версии для ARM64 и включение режима контейнеров.

Соберите архив из корня проекта:

```sh
make build-awg-arm64
docker build --platform linux/arm64 -t keenproxy-awg:local -f tools/routeros/awg.Dockerfile dist
docker save -o keenproxy-awg-arm64.tar keenproxy-awg:local
```

Импортируйте архив в RouterOS. Подключите каталог с файлом `awg.conf` в `/config` и разрешите UID/GID 65534 только чтение. Укажите для контейнера DNS-сервер, доступный с транзитного интерфейса. Это особенно важно, если адрес AWG-сервера задан именем. Запустите контейнер без привилегий с этими аргументами, заменив адреса примера на адреса выделенного транзитного интерфейса:

```text
-config /config/awg.conf -socks-listen 192.0.2.2:1080 -allow-from 192.0.2.1
```

Создайте службу Socksify, указывающую на TCP-порт 1080 контейнера. Добавьте правило destination NAT с `action=socksify`, `protocol=tcp`, `dst-address-type=!local` и явным выбором тестового клиента и назначения. Проверьте доступ тестового клиента к порту Socksify в фильтре input и исключите соответствующий TCP из FastTrack. Проверьте HTTPS с тестового и независимого клиента. Для возврата к прежней маршрутизации отключите или удалите только правило и службу, добавленные для AWG.

Перед загрузкой файла в RouterOS удалите из него ключ подписки Premium. Пример для подготовленного транзитного моста, архива и каталога с профилем. Подставьте адрес тестового клиента, назначение и адрес DNS.

```routeros
/container/mounts/add list=kp-awg-profile src=disk1/kp-awg-profile dst=/config
/container/add name=kp-awg file=keenproxy-awg-arm64.tar root-dir=disk1/kp-awg-root interface=kp-awg-veth mountlists=kp-awg-profile user=65534:65534 privileged=no logging=yes dns=192.0.2.1 cmd="-config /config/awg.conf -socks-listen 192.0.2.2:1080 -allow-from 192.0.2.1"
/container/start [find where name=kp-awg]
/ip/socksify/add name=kp-awg socks5-server=192.0.2.2 socks5-port=1080 disabled=no
/ip/firewall/nat/add chain=dstnat action=socksify protocol=tcp src-address=198.51.100.10 dst-address=203.0.113.10 dst-address-type=!local dst-port=443 socksify-service=kp-awg comment=kp-awg-pilot
```


См. [RouterOS Container](https://manual.mikrotik.com/docs/containers/) и [RouterOS Socksify](https://manual.mikrotik.com/docs/network-management/socks/socksify/).
