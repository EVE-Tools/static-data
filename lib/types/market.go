package types

import (
	"context"
	"time"

	pb "github.com/EVE-Tools/static-data/lib/staticData"
	"github.com/antihax/goesi"
	"github.com/boltdb/bolt"
	"github.com/golang/protobuf/proto"
	google_pb "github.com/golang/protobuf/ptypes/empty"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetMarketTypes returns all market type IDs from cache
func GetMarketTypes(context context.Context, empty *google_pb.Empty) (*pb.GetMarketTypesResponse, error) {
	var typesBlob []byte

	// Try to get type's IDs from BoltDB
	db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("marketTypes"))
		typesBlob = bucket.Get([]byte("ids"))
		return nil
	})

	if typesBlob == nil {
		logrus.Error("could not get type's IDs from BoltDB")
		return nil, status.Error(codes.NotFound, "Error retrieving types")
	}

	var types pb.GetMarketTypesResponse
	err := proto.Unmarshal(typesBlob, &types)
	if err != nil {
		logrus.WithError(err).Error("could not parse type IDs from BoltDB")
		return nil, status.Error(codes.NotFound, "Error parsing type's IDs")
	}

	return &types, nil
}

var db *bolt.DB
var esiClient *goesi.APIClient
var esiSemaphore chan struct{}

// Initialize initializes infrastructure for market types
func Initialize(esi *goesi.APIClient, database *bolt.DB) {
	db = database
	esiClient = esi
	esiSemaphore = make(chan struct{}, 200)

	// Initialize buckets
	err := db.Update(func(tx *bolt.Tx) error {
		tx.CreateBucketIfNotExists([]byte("marketTypes"))
		return nil
	})
	if err != nil {
		panic(err)
	}

	// Load
	go scheduleMarketTypeUpdate()
}

// Keep ticking in own goroutine and spawn worker tasks.
func scheduleMarketTypeUpdate() {
	// Load on start...
	go updateMarketTypes()

	// ...then update every 24 hours
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		<-ticker.C
		go updateMarketTypes()
	}
}

func updateMarketTypes() {
	logrus.Info("Updating market types...")

	// Get all type IDs
	ids, err := getMarketTypes()
	if err != nil {
		logrus.WithError(err).Warn("could not update market types")
		return
	}

	marketTypes := pb.GetMarketTypesResponse{
		TypeIds: ids,
	}

	blob, err := proto.Marshal(&marketTypes)
	if err != nil {
		logrus.WithError(err).Warn("could not marshal market types")
		return
	}

	db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("marketTypes"))
		if bucket == nil {
			panic("Bucket not found! This should never happen!")
		}

		err := bucket.Put([]byte("ids"), blob)
		return err
	})

	logrus.Info("Done updating market types!")
}

// Get all typeIDs from ESI
func getTypeIDs() ([]int32, error) {
	var typeIDs []int32
	params := make(map[string]interface{})
	params["page"] = int32(1)

	typeResult, _, err := esiClient.ESI.UniverseApi.GetUniverseTypes(nil, params)
	if err != nil {
		return nil, err
	}

	typeIDs = append(typeIDs, typeResult...)

	for len(typeResult) > 0 {
		params["page"] = params["page"].(int32) + 1
		typeResult, _, err = esiClient.ESI.UniverseApi.GetUniverseTypes(nil, params)
		if err != nil {
			return nil, err
		}

		typeIDs = append(typeIDs, typeResult...)
	}

	return typeIDs, nil
}

// Get all types on market
func getMarketTypes() ([]int32, error) {
	typeIDs, err := getTypeIDs()
	if err != nil {
		return nil, err
	}

	marketTypes := make(chan int32)
	nonMarketTypes := make(chan int32)
	failure := make(chan error)

	typesLeft := len(typeIDs)

	for _, id := range typeIDs {
		go checkIfMarketTypeAsyncRetry(id, marketTypes, nonMarketTypes, failure)
	}

	var marketTypeIDs []int32

	for typesLeft > 0 {
		select {
		case typeID := <-marketTypes:
			marketTypeIDs = append(marketTypeIDs, typeID)
		case <-nonMarketTypes:
		case err := <-failure:
			logrus.Warnf("Error fetching type from ESI: %s", err.Error())
		}

		typesLeft--
	}

	return marketTypeIDs, nil
}

// Async check if market type, retry 3 times
func checkIfMarketTypeAsyncRetry(typeID int32, marketTypes chan int32, nonMarketTypes chan int32, failure chan error) {
	var isMarketType bool
	var err error
	retries := 3

	for retries > 0 {
		isMarketType, err = checkIfMarketType(typeID)
		if err != nil {
			logrus.WithError(err).Warn("error loading type info")
			retries--
		} else {
			err = nil
			retries = 0
		}
	}

	if err != nil {
		failure <- err
		return
	}

	if isMarketType {
		marketTypes <- typeID
		return
	}

	nonMarketTypes <- typeID
}

// Check if type is market type
func checkIfMarketType(typeID int32) (bool, error) {
	esiSemaphore <- struct{}{}
	typeInfo, _, err := esiClient.ESI.UniverseApi.GetUniverseTypesTypeId(nil, typeID, nil)
	<-esiSemaphore
	if err != nil {
		return false, err
	}

	// If it is published and has a market group it is a market type!
	if typeInfo.Published && (typeInfo.MarketGroupId != 0) {
		return true, nil
	}

	return false, nil
}
