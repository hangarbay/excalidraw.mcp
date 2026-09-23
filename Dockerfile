FROM node:26-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
COPY internal/webui/ /src/internal/webui/
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /excalidraw-mcp ./cmd/excalidraw-mcp

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /excalidraw-mcp /excalidraw-mcp
EXPOSE 3210
ENTRYPOINT ["/excalidraw-mcp", "http", "-listen", "0.0.0.0:3210", "-scene", "-"]
