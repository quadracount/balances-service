FROM golang:1-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/server .

FROM alpine:3
COPY --from=build /app/server /server
EXPOSE 50051
ENTRYPOINT ["/server"]