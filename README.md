# Overview

Go utility that updates your openDNS IP address for DDNS using the DNS-O-Matic API. 

The app gets your current public IP address and compares that to your last known public IP and if they differ it calls the DNS-O-Matic API to update your IP address. 

https://www.dnsomatic.com/docs/api

### App Configuration

The app runs on a time interval that you specify via env vars or a .env file. View the .env file in the root of the repo for a list of the env vars that can be injected into the container. 

# Run Locally

First, build the application...
```go build -o ddns-updater```

Then, run it using the .env file...

```./ddns-updater --env=./env```

# Run in Docker

```docker build --tag ddns-updater:latest .```

```docker run --env DDNS_BASEHOST=myip.dnsomatic.com --env DDNS_UPDHOST=updates.dnsomatic.com --env DDNS_UN=[YOUR OPENDNS EMAIL] --env DDNS_PW=[YOUR OPENDNS PASSWORD] --env DDNS_DNSHOST=home --env DDNS_WILDCARD=NOCHG --env DDNS_MX=NOCHG --env DDNS_BACKMX=NOCHG --env DDNS_DBPATH=./persistence/ip.db --env DDNS_INTERVAL=120s --env DDNS_DEBUG=true ddns-updater:latest```