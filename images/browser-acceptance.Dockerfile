FROM node:24.18.0-bookworm@sha256:5711a0d445a1af54af9589066c646df387d1831a608226f4cd694fc59e745059

ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright

RUN npm install --global --ignore-scripts @playwright/cli@0.1.17 \
    && node /usr/local/lib/node_modules/@playwright/cli/node_modules/playwright/cli.js install --with-deps chromium \
    && chmod -R a+rX /ms-playwright /usr/local/lib/node_modules/@playwright

WORKDIR /src
USER 1000:1000
CMD ["./test/e2e/ui-smoke.sh", "http://127.0.0.1:8080"]
