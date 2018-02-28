# Static Data
[![Build Status](https://drone.element-43.com/api/badges/EVE-Tools/static-data/status.svg)](https://drone.element-43.com/EVE-Tools/static-data) [![Go Report Card](https://goreportcard.com/badge/github.com/eve-tools/static-data)](https://goreportcard.com/report/github.com/eve-tools/static-data) [![Docker Image](https://images.microbadger.com/badges/image/evetools/static-data.svg)](https://microbadger.com/images/evetools/static-data)

This service for [Element43](https://element-43.com) handles all (bulk) requests for static data we currently cannot do via [ESI](https://esi.tech.ccp.is/latest/). At the moment this is restricted to serving market type's IDs and uniform location data regarding structures/stations, solar systems, constellations and regions, acting as a kind of best-effort (more on that later) caching proxy for external APIs. Typical requests query around 1,000 locations. Location data is fetched from multiple sources, cached in-memory and persisted to disk. This prevents unnecessary requests to external APIs. Depending on the location's ID, different sources and cache exiprations are used:

1. Stations, Solar Systems, Constellations, Regions: ESI, 24h expiry
2. Conquerable Stations: ESI, 1h expiry
3. Structures (citadels...): [3rd Party API](https://stop.hammerti.me.uk/citadelhunt/getstarted), fetched in bulk every hour

Items are not deleted on expiry as the APIs can be flaky or down for extended periods of time. In case a queried entry is expired the proxy tries to retrieve location info for the entry. If the backing API is down, the expired entry is served as a fallback.

Issues can be filed [here](https://github.com/EVE-Tools/element43). Pull requests can be made in this repo.

## Interface
The service's gRPC description can be found [here](https://github.com/EVE-Tools/element43/blob/master/services/staticData/staticData.proto).

## Installation
Either use the prebuilt Docker images and pass the appropriate env vars (see below), or:

* Install Go, clone this repo into your gopath
* Run `go get ./...` to fetch the service's dependencies
* Run `bash generateProto.sh` to generate the necessary gRPC-related code
* Run `go build` to build the service
* Run `./static-data` to start the service


## Deployment Info
Builds and releases are handled by Drone.

Environment Variable | Default | Description
--- | --- | ---
LOG_LEVEL | info | Threshold for logging messages to be printed
PORT | 43000 | Port for the API to listen on
DB_PATH | static-data.db | Path for storing the persistent location cache
ESI_HOST | esi.tech.ccp.is | Hostname used for accessing ESI. Change this if you proxy requests. 
STRUCTURE_HUNT_HOST | stop.hammerti.me.uk | Hostname used for accessing the 3rd party structure hunt API. Change this if you proxy requests.
DISABLE_TLS | false | Only check this if you're proxying API requests and terminate TLS-connections at the proxy.
