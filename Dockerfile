#
# Build go project
#
FROM golang:1.23-alpine as go-builder

WORKDIR /ecs-upgrade/

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /ecs-upgrade/ecs-upgrade *.go

#
# Runtime container
#
FROM alpine:3.18.3  

RUN apk --no-cache add ca-certificates && mkdir -p /app

WORKDIR /app

COPY --from=go-builder /ecs-upgrade/ecs-upgrade .

CMD ["./ecs-upgrade"]  
