FROM golang:alpine AS stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download 

COPY main.go ./

RUN go build -o ddns-updater

FROM golang:alpine 

WORKDIR /app
RUN mkdir persistence

COPY --from=stage /app/ddns-updater .
COPY ./persistence/ip.db ./persistence/

CMD ["./ddns-updater"]
