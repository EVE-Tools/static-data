# Static Data
[![Build Status](https://drone.element-43.com/api/badges/EVE-Tools/static-data/status.svg)](https://drone.element-43.com/EVE-Tools/static-data) [![Go Report Card](https://goreportcard.com/badge/github.com/eve-tools/static-data)](https://goreportcard.com/report/github.com/eve-tools/static-data)

This service for [Element43](https://element-43.com) handles all (bulk) requests for static data we currently cannot do via [ESI](https://esi.tech.ccp.is/latest/). At the moment this is restricted to serving uniform location data regarding structures/stations, solar systems, constellations and regions, acting as a kind of best-effort (more on that later) caching proxy for external APIs. Typical requests query around 1,000 locations. Location data is fetched from multiple sources, cached in-memory and persisted to disk. This prevents unnecessary requests to external APIs. Depending on the location's ID, different sources and cache exiprations are used:

1. Stations, Solar Systems, Constellations, Regions: CREST, 24h expiry
2. Conquerable Stations: CREST, 1h expiry
3. Structures (citadels...): [3rd Party API](https://stop.hammerti.me.uk/citadelhunt/getstarted), fetched in bulk every hour

Items are not deleted on expiry as the APIs can be flaky or down for extended periods of time. In case a queried entry is expired the proxy tries to retrieve location info for the entry. If the backing API is down, the expired entry is served as a fallback.

## Installation
Either use the prebuilt Docker images and pass the appropriate env vars (see below), or:

* Clone this repo into your gopath
* Run `go get`
* Run `go build`


## Deployment Info
Builds and releases are handled by Drone.

Environment Variable | Default | Description
--- | --- | ---
LOG_LEVEL | info | Threshold for logging messages to be printed
PORT | 8000 | Port for the API to listen on
DB_PATH | static-data.db | Path for storing the persistent location cache

## Todo
- [ ] Seriously, this should not need to exist at all, maybe replace it with something like [Falcor](https://github.com/Netflix/falcor)

## Endpoints

Prefix: `/api/static-data/v1`

URL Pattern | Description
--- | ---
`/region/location/` | POST a JSON object with a key called `locationIDs` containing all the IDs you need info for. It will return the info. Magic!

Example output for `{ "locationIDs": [60003760, 30003271, 1022449681307] }`:
```json
{
  "30003271": {
    "station": {
      "id": 0,
      "name": "",
      "position": {
        "x": 0,
        "y": 0,
        "z": 0
      }
    },
    "region": {
      "id": 10000041,
      "name": "Syndicate"
    },
    "solarSystem": {
      "id": 30003271,
      "securityStatus": -0.019552214071154594,
      "name": "Poitot"
    },
    "constellation": {
      "id": 20000478,
      "name": "Z-6NQ6"
    }
  },
  "60003760": {
    "station": {
      "id": 60003760,
      "name": "Jita IV - Moon 4 - Caldari Navy Assembly Plant",
      "position": {
        "x": 0,
        "y": 0,
        "z": 0
      }
    },
    "region": {
      "id": 10000002,
      "name": "The Forge"
    },
    "solarSystem": {
      "id": 30000142,
      "securityStatus": 0.9459131360054016,
      "name": "Jita"
    },
    "constellation": {
      "id": 20000020,
      "name": "Kimotoro"
    }
  },
  "1022449681307": {
    "station": {
      "id": 1022449681307,
      "name": "Safshela - АТАС",
      "typeId": 35832,
      "typeName": "Astrahus",
      "lastSeen": "2017-01-04T13:03:07Z",
      "firstSeen": "2016-11-09T20:31:52Z",
      "position": {
        "x": 982752948061,
        "y": 135584807000,
        "z": 3980081299879
      }
    },
    "region": {
      "id": 10000049,
      "name": "Khanid"
    },
    "solarSystem": {
      "id": 30003879,
      "securityStatus": 0.6676156520843506,
      "name": "Safshela"
    },
    "constellation": {
      "id": 20000566,
      "name": "Amdimmah"
    }
  }
}
```
