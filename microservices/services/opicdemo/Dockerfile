FROM golang:1.25-alpine AS build
WORKDIR /src
COPY services/go.mod services/go.sum ./
RUN go mod download
COPY services/ ./
RUN CGO_ENABLED=0 go build -o /out/opicdemo ./opicdemo/cmd

FROM alpine:3.20
RUN addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=build /out/opicdemo /app/opicdemo
COPY services/opicdemo/migrations /app/migrations
COPY services/opicdemo/models /app/models
USER app
EXPOSE 9700
ENTRYPOINT ["/app/opicdemo"]
