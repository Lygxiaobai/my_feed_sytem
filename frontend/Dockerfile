#
# Multi-stage build for Vue (Vite) frontend, served by Nginx.
#
# Build:
#   docker build -f frontend/Dockerfile -t feedsystem-frontend .
#

FROM node:24-alpine AS build
WORKDIR /src

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

FROM nginx:1.27-alpine
RUN mkdir -p /etc/nginx/snippets /var/www/certbot
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY frontend/nginx-upload-api.conf /etc/nginx/snippets/upload-api.conf
COPY frontend/nginx-silo-upload.conf /etc/nginx/snippets/silo-upload.conf
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 80 443 8443
CMD ["nginx", "-g", "daemon off;"]

