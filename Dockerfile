FROM golang:1.22.0-alpine as build

RUN apk upgrade

# Set the Current Working Directory inside the container
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY view/* ./view/

RUN go build -o main .

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=build /app/main /app/main

EXPOSE 8080
CMD [ "./main" ]