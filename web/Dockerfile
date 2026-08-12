FROM node:26-bookworm AS build

WORKDIR /web

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

FROM caddy:2.10-alpine

COPY Caddyfile.production /etc/caddy/Caddyfile
COPY --from=build /web/dist /srv

EXPOSE 80 443
