package locations

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/golang/protobuf/ptypes"

	"fmt"

	"io/ioutil"

	pb "github.com/EVE-Tools/static-data/lib/staticData"
	"github.com/antihax/goesi"
	"github.com/boltdb/bolt"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// GetLocations returns location info for a given list.
func GetLocations(context context.Context, request *pb.GetLocationsRequest) (*pb.GetLocationsResponse, error) {
	locations, _ := getLocations(request.GetLocationIds())

	return &pb.GetLocationsResponse{Locations: locations}, nil
}

var db *bolt.DB
var esiClient *goesi.APIClient
var genericClient *http.Client
var structureHuntURL string

// Initialize initializes infrastructure for locations
func Initialize(esi *goesi.APIClient, gen *http.Client, url string, database *bolt.DB) {
	db = database
	esiClient = esi
	genericClient = gen
	structureHuntURL = url

	// Initialize buckets
	err := db.Update(func(tx *bolt.Tx) error {
		tx.CreateBucketIfNotExists([]byte("locations"))
		return nil
	})
	if err != nil {
		panic(err)
	}

	// Initialize static data
	go scheduleStaticDataUpdate()
}

// Keep ticking in own goroutine and spawn worker tasks.
func scheduleStaticDataUpdate() {
	// Load on start...
	go updateStructures()
	go updateRegions()

	// ...then update every 30 minutes
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		<-ticker.C
		go updateStructures()
		go updateRegions()
	}
}

// Update all structures in cache
func updateStructures() {
	logrus.Debug("Downloading structures...")

	// Fetch with no timeout
	requestStart := time.Now()
	response, err := genericClient.Get(structureHuntURL)
	requestTime := time.Since(requestStart)
	logrus.WithFields(logrus.Fields{
		"time": requestTime,
	}).Info("Loaded structures.")
	if err != nil {
		logrus.WithError(err).Warn("Could not fetch 3rd party structure API")
		return
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		logrus.Warnf("Server returned invalid status: %d", response.StatusCode)
		return
	}

	var allStructures AllStructures

	responseJSON, err := ioutil.ReadAll(response.Body)
	if err != nil {
		logrus.WithError(err).Warn("Could not read response of structure hunt")
		return
	}

	err = allStructures.UnmarshalJSON(responseJSON)
	if err != nil {
		logrus.WithError(err).Warn("JSON returned from 3rd party structure API is invalid")
		return
	}

	logrus.Debugf("OK! Got %d structures.", len(allStructures))

	// Take each structure and fetch solar system info, then store in DB
	systemIDs := make([]int64, len(allStructures))
	var i int
	for _, structure := range allStructures {
		systemIDs[i] = structure.SystemID
		i++
	}

	_, err = getLocations(systemIDs)
	if err != nil {
		logrus.WithError(err).Warnf("Failed to update structure cache")
		return
	}

	// Store structures in cache (expire after 1 day, this has no effect)
	expireAt := time.Now().Unix() + 86400
	for key, structure := range allStructures {
		go storeStructure(key, structure, expireAt)
	}
}

// Update all structures in cache
func updateRegions() {
	logrus.Debug("Downloading regions...")

	// Fetch IDs from ESI
	regionIDs, _, err := esiClient.ESI.UniverseApi.GetUniverseRegions(nil, nil)
	if err != nil {
		logrus.WithError(err).Error("Could not get regions.")
		return
	}

	for _, id := range regionIDs {
		region, _, err := esiClient.ESI.UniverseApi.GetUniverseRegionsRegionId(nil, id, nil)
		if err != nil {
			logrus.WithError(err).Error("Could not get region info.")
			return
		}

		// Store structures in cache (expire after 1 day, this has no effect)
		cachedLocation := CachedLocation{
			ID:        int64(region.RegionId),
			ExpiresAt: time.Now().Unix() + 86400,
			Location: pb.Location{
				Region: &pb.Region{
					Id:   int64(region.RegionId),
					Name: region.Name,
				},
			},
		}

		err = putIntoCache(cachedLocation)
		if err != nil {
			logrus.WithError(err).Warn("Failed to store region")
			return
		}
	}
}

func storeStructure(key string, structure Structure, expireAt int64) {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		logrus.WithError(err).Warnf("Failed to parse structure ID")
		return
	}

	system, err := getLocation(structure.SystemID)
	if err != nil {
		logrus.WithError(err).Warnf("Failed to fetch system")
		return
	}

	lastSeenProto, err := ptypes.TimestampProto(structure.LastSeen)
	if err != nil {
		logrus.WithError(err).Warnf("Could not convert structure's last seen timestamp")
		return
	}

	firstSeenProto, err := ptypes.TimestampProto(structure.FirstSeen)
	if err != nil {
		logrus.WithError(err).Warnf("Could not convert structure's first seen timestamp")
		return
	}

	cachedLocation := CachedLocation{
		ID:        id,
		ExpiresAt: expireAt,
		Location: pb.Location{
			Region:        system.Region,
			Constellation: system.Constellation,
			SolarSystem:   system.SolarSystem,
			Station: &pb.Station{
				Id:          id,
				Name:        structure.Name,
				TypeId:      structure.TypeID,
				TypeName:    structure.TypeName,
				LastSeen:    lastSeenProto,
				Public:      structure.Public,
				FirstSeen:   firstSeenProto,
				Coordinates: &structure.Coordinates,
			},
		},
	}

	err = putIntoCache(cachedLocation)
	if err != nil {
		logrus.WithError(err).Warnf("Failed to store structure")
		return
	}
}

// Get a single location.
func getLocation(id int64) (pb.Location, error) {
	cachedLocation, err := getCachedLocation(id)

	if err != nil {
		return pb.Location{}, err
	}

	return cachedLocation.Location, nil
}

// Get multiple locations by ID in parallel and return them as map indexed by ID, on error return partial result.
func getLocations(ids []int64) (map[int64]*pb.Location, error) {
	// Deduplicate IDs
	ids = deduplicateIDs(ids)

	response := make(map[int64]*pb.Location)
	success := make(chan CachedLocation)
	failure := make(chan error)
	outstandingRequests := len(ids)
	failed := false

	for _, id := range ids {
		go getLocationAsync(id, success, failure)
	}

	for outstandingRequests > 0 {
		select {
		case location := <-success:
			response[location.ID] = &location.Location
		case err := <-failure:
			logrus.Warn(err.Error())
			failed = true
		}

		outstandingRequests--
	}

	if failed {
		return response, errors.New("could not fetch all locations")
	}

	return response, nil
}

func getLocationAsync(id int64, success chan CachedLocation, failure chan error) {
	location, err := getCachedLocation(id)
	if err != nil {
		failure <- err
		return
	}
	success <- location
}

/* Try to get location from cache, if not present or outdated, update location from backend.
   If backend fails, return cached version. Only error if even backend-fetching failed. */
func getCachedLocation(id int64) (CachedLocation, error) {
	// Fetch from cache
	location, needsUpdate, err := fetchLocationFromCache(id)
	if err != nil {
		return location, err
	}

	// Check if it needs an update
	if needsUpdate {
		location, err = updateLocationInCache(id)

		if err != nil {
			return location, err
		}
	}

	if location == (CachedLocation{}) {
		msg := fmt.Sprintf("could not get a valid location %d from cache and backend", id)
		return location, errors.New(msg)
	}

	return location, nil
}

// Try to fetch location from cache and test if it needs to be updated.
func fetchLocationFromCache(id int64) (location CachedLocation, needsUpdate bool, err error) {
	var serializedLocation []byte
	db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("locations"))
		if bucket == nil {
			panic("Bucket not found! This should never happen!")
		}

		serializedLocation = bucket.Get([]byte(strconv.FormatInt(id, 10)))
		return nil
	})

	if serializedLocation == nil {
		return CachedLocation{}, true, nil
	}

	var cachedLocation CachedLocation
	err = cachedLocation.UnmarshalJSON(serializedLocation)
	if err != nil {
		return CachedLocation{}, true, err
	}

	// Check if location needs update, citadels and regions are updated via ticker
	if cachedLocation.ID > 20000000 && cachedLocation.ID < 1000000000000 && cachedLocation.ExpiresAt < time.Now().Unix() {
		return cachedLocation, true, nil
	}

	return cachedLocation, false, nil
}

// Fetch a single location from backend and put it into cache.
func updateLocationInCache(id int64) (CachedLocation, error) {
	// Exclude citadels as they are updated in bulk via ticker
	if id > 1000000000000 {
		// This only happens if someone queries a citadel which is unknown
		msg := fmt.Sprintf("Could not find citadel %d in current dataset", id)
		return CachedLocation{}, errors.New(msg)
	}

	// Exclude implausible ID ranges
	if id < 10000000 || id > 64000000 || (id >= 40000000 && id < 60000000) {
		logrus.Debug(id)
		return CachedLocation{}, errors.New("not a valid location ID range")
	}

	// Rest of requests are requests to ESI's location API.
	rawLocation, err := fetchLocationFromESI(id)
	if err != nil {
		return CachedLocation{}, err
	}

	var expireAt int64
	if id > 61000000 {
		// Conquerable stations expire after an hour
		expireAt = time.Now().Unix() + 3600
	} else {
		// Normal stations expire after a day
		expireAt = time.Now().Unix() + 86400
	}

	cachedLocation := CachedLocation{
		ID:        id,
		ExpiresAt: expireAt,
		Location:  rawLocation,
	}

	err = putIntoCache(cachedLocation)

	return cachedLocation, err
}

// Put a cachedLocation into cache
func putIntoCache(cachedLocation CachedLocation) error {
	logrus.Debugf("Storing location %d in cache", cachedLocation.ID)

	var cachedLocationJSON []byte
	cachedLocationJSON, err := cachedLocation.MarshalJSON()
	if err != nil {
		return err
	}

	// Batch calls as we're probably running this concurrently for lots of requests.
	err = db.Batch(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("locations"))
		if bucket == nil {
			panic("Bucket not found! This should never happen!")
		}
		key := []byte(strconv.FormatInt(cachedLocation.ID, 10))
		err = bucket.Put(key, cachedLocationJSON)
		return err
	})

	if err != nil {
		return err
	}

	return nil
}

// Fetches a location from ESI.
func fetchLocationFromESI(id int64) (pb.Location, error) {
	logrus.Debugf("Getting location %d from ESI", id)

	// Check location type
	locationType, response, err := esiClient.ESI.UniverseApi.PostUniverseNames(nil, []int32{int32(id)}, nil)
	if err != nil {
		msg := fmt.Sprintf("could not get location type of ID %d from ESI", id)
		return pb.Location{}, errors.Wrap(err, msg)
	}
	if response.StatusCode != http.StatusOK {
		msg := "Invalid HTTP status when querying location type!"
		return pb.Location{}, errors.New(msg)
	}
	if len(locationType) == 0 {
		msg := "No location type returned by ESI!"
		return pb.Location{}, errors.New(msg)
	}

	// Get and return location
	switch locationType[0].Category {
	case "station":
		return fetchStation(id)
	case "solar_system":
		return fetchSolarSystem(id)
	case "constellation":
		return fetchConstellation(id)
	case "region":
		return fetchRegion(id)
	default:
		msg := fmt.Sprintf("Unhandled category '%s'!", locationType[0].Category)
		return pb.Location{}, errors.New(msg)
	}
}

// Fetch a station from ESI
func fetchStation(id int64) (pb.Location, error) {
	// Check if recent version is available in cache
	cachedStation, needsUpdate, err := fetchLocationFromCache(id)
	if err != nil {
		return pb.Location{}, err
	}

	// Return cached version
	if !needsUpdate {
		return cachedStation.Location, nil
	}

	logrus.WithField("station_id", id).Debug("Loading station from ESI.")

	// Fetch from ESI if not in cache
	station, _, err := esiClient.ESI.UniverseApi.GetUniverseStationsStationId(nil, int32(id), nil)
	if err != nil {
		return pb.Location{}, err
	}

	// Get solar system
	solarSystem, err := fetchSolarSystem(int64(station.SystemId))
	if err != nil {
		return pb.Location{}, err
	}

	coordinates := pb.Coordinates{
		X: float64(station.Position.X),
		Y: float64(station.Position.Y),
		Z: float64(station.Position.Z),
	}

	location := pb.Location(solarSystem)
	location.Station = &pb.Station{
		Id:          int64(station.StationId),
		Name:        station.Name,
		TypeId:      int64(station.TypeId),
		Public:      true,
		Coordinates: &coordinates,
	}

	return location, nil
}

// Fetch a solar system from ESI
func fetchSolarSystem(id int64) (pb.Location, error) {
	// Check if recent version is available in cache
	cachedSolarSystem, needsUpdate, err := fetchLocationFromCache(id)
	if err != nil {
		return pb.Location{}, err
	}

	// Return cached version
	if !needsUpdate {
		return cachedSolarSystem.Location, nil
	}

	logrus.WithField("solar_system_id", id).Debug("Loading solar system from ESI.")

	// Fetch from ESI if not in cache
	solarSystem, _, err := esiClient.ESI.UniverseApi.GetUniverseSystemsSystemId(nil, int32(id), nil)
	if err != nil {
		return pb.Location{}, err
	}

	// Get constellation
	constellation, err := fetchConstellation(int64(solarSystem.ConstellationId))
	if err != nil {
		return pb.Location{}, err
	}

	location := pb.Location(constellation)
	location.SolarSystem = &pb.SolarSystem{
		Id:             int64(solarSystem.SystemId),
		Name:           solarSystem.Name,
		SecurityStatus: float64(solarSystem.SecurityStatus),
	}

	return location, nil
}

// Fetch a constellation from ESI
func fetchConstellation(id int64) (pb.Location, error) {
	// Check if recent version is available in cache
	cachedConstellation, needsUpdate, err := fetchLocationFromCache(id)
	if err != nil {
		return pb.Location{}, err
	}

	// Return cached version
	if !needsUpdate {
		return cachedConstellation.Location, nil
	}

	logrus.WithField("constellation_id", id).Debug("Loading constellation from ESI.")

	// Fetch from ESI if not in cache
	constellation, _, err := esiClient.ESI.UniverseApi.GetUniverseConstellationsConstellationId(nil, int32(id), nil)
	if err != nil {
		return pb.Location{}, err
	}

	// Get region
	region, err := fetchRegion(int64(constellation.RegionId))
	if err != nil {
		return pb.Location{}, err
	}

	location := pb.Location(region)
	location.Constellation = &pb.Constellation{
		Id:   int64(constellation.ConstellationId),
		Name: constellation.Name,
	}

	return location, nil
}

// Fetch a region from ESI
func fetchRegion(id int64) (pb.Location, error) {
	// Check if recent version is available in cache
	cachedRegion, needsUpdate, err := fetchLocationFromCache(id)
	if err != nil {
		return pb.Location{}, err
	}

	// Return cached version
	if !needsUpdate {
		return cachedRegion.Location, nil
	}

	logrus.WithField("region_id", id).Debug("Loading region from ESI.")

	// Fetch from ESI if not in cache
	region, _, err := esiClient.ESI.UniverseApi.GetUniverseRegionsRegionId(nil, int32(id), nil)
	if err != nil {
		return pb.Location{}, err
	}

	return pb.Location{
		Region: &pb.Region{
			Id:   int64(region.RegionId),
			Name: region.Name,
		},
	}, nil
}

// Deduplicate a slice of integers
func deduplicateIDs(ids []int64) []int64 {
	// This is a small trick for deduplicating IDs: Simply create a map
	// and use it as a set by mapping the keys to empty values, then re-add
	// keys to target slice. The map has constant lookup time, so adding the
	// keys is really fast and the size of the target slice is already determined
	// by the map. This is more efficient than a naïve algorithm.
	idSet := make(map[int64]struct{})
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	var i int
	uniqueIDs := make([]int64, len(idSet))
	for id := range idSet {
		uniqueIDs[i] = id
		i++
	}

	return uniqueIDs
}
