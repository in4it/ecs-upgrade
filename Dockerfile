#
# Build go project
#
FROM golang:1.10-alpine as go-builder

WORKDIR /go/src/github.com/in4it/ecs-upgrade/

COPY . .

RUN curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh && dep ensure
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o ecs-upgrade *.go

#
# Runtime container
#
FROM alpine:latest  

ARG SOURCE_COMMIT=unknown

RUN apk --no-cache add ca-certificates && mkdir -p /app

WORKDIR /app

COPY --from=go-builder /go/src/github.com/in4it/ecs-upgrade/ecs-upgrade .

RUN echo ${SOURCE_COMMIT} > source_commit

CMD ["./ecs-upgrade"]  
