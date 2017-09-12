package locations

import (
	"strconv"
	"time"

	"errors"

	"fmt"

	"io/ioutil"

	"github.com/Sirupsen/logrus"
	"github.com/antihax/goesi"
	"github.com/boltdb/bolt"
	"github.com/gin-gonic/gin"
	"github.com/valyala/fasthttp"
)

// GetLocationsEndpoint returns location info for a given list.
func GetLocationsEndpoint(context *gin.Context) {
	// Get request body
	body, err := ioutil.ReadAll(context.Request.Body)
	if err != nil {
		context.AbortWithError(500, err)
		return
	}

	// Parse IDs from body
	var ids RequestLocationsBody
	err = ids.UnmarshalJSON(body)
	if err != nil {
		context.AbortWithError(500, err)
		return
	}

	// Try to get from cache, even if none returned, return code 207
	locations, cacheErr := getLocations(ids.Locations)

	//  Serialize data from cache
	responseJSON, err := locations.MarshalJSON()
	if err != nil {
		context.AbortWithError(500, err)
		return
	}

	// If there was an error while fetching data, return successful subset with 207, on succcess 200
	responseCode := 200
	if cacheErr != nil {
		responseCode = 207
	}

	context.Data(responseCode, "application/json; charset=utf-8", responseJSON)
}

var db *bolt.DB
var esiClient goesi.APIClient
var genericClient fasthttp.Client
var crestClient fasthttp.HostClient
var crestSemaphore chan struct{}

// Initialize initializes infrastructure for locations
func Initialize(database *bolt.DB) {
	db = database

	// Initialize buckets
	err := db.Update(func(tx *bolt.Tx) error {
		tx.CreateBucketIfNotExists([]byte("locations"))
		return nil
	})
	if err != nil {
		panic(err)
	}

	// Initialize clients
	userAgent := "Element43/static-data (element-43.com)"

	genericClient.Name = userAgent

	crestSemaphore = make(chan struct{}, 20)
	crestClient.Addr = "crest-tq.eveonline.com:443"
	crestClient.Name = userAgent
	crestClient.IsTLS = true
	crestClient.MaxConns = 20
	crestClient.ReadTimeout = 2 * time.Second

	esiClient = *goesi.NewAPIClient(nil, userAgent)

	// Initialize static data
	go scheduleStaticDataUpdate()
}

// Keep ticking in own goroutine and spawn worker tasks.
func scheduleStaticDataUpdate() {
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
	var body []byte

	logrus.Debug("Downloading structures...")

	// Fetch with no timeout
	requestStart := time.Now()
	statusCode, body, err := genericClient.Get(body, "https://stop.hammerti.me.uk/api/structure/all")
	requestTime := time.Since(requestStart)
	logrus.WithFields(logrus.Fields{
		"time": requestTime,
	}).Info("Loaded structures.")
	if err != nil {
		logrus.Warnf("Could not fetch 3rd party structure API: %s", err.Error())
		return
	}
	if statusCode != 200 {
		logrus.Warnf("Server returned invalid status: %d", statusCode)
		return
	}

	var allStructures AllStructures
	err = allStructures.UnmarshalJSON(body)
	if err != nil {
		logrus.Warnf("JSON returned from 3rd party structure API is invalid: %s", err.Error())
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
		logrus.Warnf("Failed to update structure cache: %s", err.Error())
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
	regionIDs, _, err := esiClient.V1.UniverseApi.GetUniverseRegions(nil)
	if err != nil {
		logrus.WithError(err).Error("Could not get regions.")
		return
	}

	for _, id := range regionIDs {
		region, _, err := esiClient.V1.UniverseApi.GetUniverseRegionsRegionId(id, nil)
		if err != nil {
			logrus.WithError(err).Error("Could not get region info.")
			return
		}

		// Store structures in cache (expire after 1 day, this has no effect)
		cachedLocation := CachedLocation{
			ID:        int64(region.RegionId),
			ExpiresAt: time.Now().Unix() + 86400,
			Location: Location{
				Region: Region{
					ID:   int64(region.RegionId),
					Name: region.Name,
				},
			},
		}

		err = putIntoCache(cachedLocation)
		if err != nil {
			logrus.Warnf("Failed to store region: %s", err.Error())
			return
		}
	}
}

func storeStructure(key string, structure Structure, expireAt int64) {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		logrus.Warnf("Failed to parse structure ID: %s", err.Error())
		return
	}

	system, err := getLocation(structure.SystemID)
	if err != nil {
		logrus.Warnf("Failed to fetch system: %s", err.Error())
		return
	}

	cachedLocation := CachedLocation{
		ID:        id,
		ExpiresAt: expireAt,
		Location: Location{
			Region:        system.Region,
			Constellation: system.Constellation,
			SolarSystem:   system.SolarSystem,
			Station: Station{
				ID:          id,
				Name:        structure.Name,
				TypeID:      structure.TypeID,
				TypeName:    structure.TypeName,
				LastSeen:    structure.LastSeen,
				Public:      structure.Public,
				FirstSeen:   structure.FirstSeen,
				Coordinates: structure.Coordinates,
			},
		},
	}

	err = putIntoCache(cachedLocation)
	if err != nil {
		logrus.Warnf("Failed to store structure: %s", err.Error())
		return
	}
}

// Get a single location.
func getLocation(id int64) (Location, error) {
	cachedLocation, err := getCachedLocation(id)

	if err != nil {
		return Location{}, err
	}

	return cachedLocation.Location, nil
}

// Get multiple locations by ID in parallel and return them as map indexed by ID, on error return partial result.
func getLocations(ids []int64) (Response, error) {
	// Deduplicate IDs
	ids = deduplicateIDs(ids)

	response := make(Response)
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
			response[strconv.FormatInt(location.ID, 10)] = location.Location
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
	if id < 30000000 || id > 70000000 || (id >= 40000000 && id < 60000000) {
		logrus.Debug(id)
		return CachedLocation{}, errors.New("not a valid location ID range")
	}

	// Rest of requests are requests to CREST's location API.
	rawLocation, err := fetchLocationFromCREST(id)
	if err != nil {
		return CachedLocation{}, err
	}

	var expireAt int64
	if id > 6100000 {
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

// Fetches a location from CREST.
func fetchLocationFromCREST(id int64) (Location, error) {
	logrus.Debugf("Getting location %d from CREST", id)

	var body []byte
	url := fmt.Sprintf("https://crest-tq.eveonline.com/universe/locations/%d/", id)
	crestSemaphore <- struct{}{}
	requestStart := time.Now()
	statusCode, body, err := crestClient.Get(body, url)
	requestTime := time.Since(requestStart)
	logrus.WithFields(logrus.Fields{
		"locationID": id,
		"time":       requestTime,
	}).Info("Got location from CREST.")
	<-crestSemaphore
	if err != nil {
		return Location{}, err
	}
	if statusCode != 200 {
		msg := fmt.Sprintf("CREST returned wrong status code %d for location %d", statusCode, id)
		return Location{}, errors.New(msg)
	}

	var location Location
	err = location.UnmarshalJSON(body)
	if err != nil {
		msg := fmt.Sprintf("CREST returned invalid JSON data for location %d (%s): %s", statusCode, err.Error(), string(body))
		return Location{}, errors.New(msg)
	}

	return location, nil
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
