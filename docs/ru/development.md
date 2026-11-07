# Разработка документации

## Локальная сборка

Потребуются Python 3.12+ и Node 22.12+:

```sh
python3 -m venv .venv-docs
.venv-docs/bin/pip install -r requirements-docs.txt
DOCS_SITE_URL=http://localhost:8097/ .venv-docs/bin/mkdocs build --strict
cd web
npm ci
npm test
npm run build:demo
cd ..
mkdir -p site/demo
cp -R web/dist-demo/. site/demo/
python3 -m http.server 8097 --directory site
```

Откройте `http://localhost:8097/` для английского, `/ru/` для русского и `/demo/` для отдельного демо. Сервер раздаёт только статические файлы. `npm run build` по-прежнему собирает интерфейс роутера в `web/dist`, а `build:demo` — в `web/dist-demo`.

## Редактирование и переводы

Поддерживайте одинаковые Markdown-файлы в `docs/en/` и `docs/ru/`. Навигация задаётся в `mkdocs.yml`; i18n-плагин добавляет переключение языка, перевод навигации и поиск. Скриншоты на обоих языках сохраняйте в `docs/assets/screenshots/`. Снимайте только синтетические сеансы и обновляйте подписи при изменении экранов.

Demo-режим подменяет импорт API на `web/src/demo/api.ts` через alias Vite. Проверка `satisfies` выявляет расхождения с обычным API. При расширении адаптера добавляйте поведенческие тесты; не имитируйте функции настоящими сетевыми запросами.

## GitHub Pages

Workflow **Documentation** собирает и проверяет оба варианта при подходящих PR и push. Загружается только `site/`: сгенерированная документация и демо. Внутренние отчёты, память агента, source maps приложения, реквизиты и рабочее состояние Go-демона в эту папку не входят.

Для публикации включите GitHub Pages и выберите источник **GitHub Actions**. Вручную запустите **Documentation** на `main` с параметром **Deploy to GitHub Pages**. PR только собирается и не может публиковаться. Адрес по умолчанию: `https://atrevido.github.io/keen-proxy/`. Для приватного репозитория нужен подходящий тариф GitHub; публикация Pages может сделать сайт общедоступным. Не меняйте видимость репозитория только ради Pages.

Бинарные релизы и публикация Pages независимы. Правила версий находятся в [CONTRIBUTING](https://github.com/atrevido/keen-proxy/blob/main/CONTRIBUTING.md), выпуск бинарников — в [RELEASING](https://github.com/atrevido/keen-proxy/blob/main/RELEASING.md).
