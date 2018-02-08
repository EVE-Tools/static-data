package locations

import (
	"time"

	pb "github.com/EVE-Tools/static-data/lib/staticData"
)

//
// Own types
//

// CachedLocation represents a location chache entry with expiration date.
type CachedLocation struct {
	ID        int64       `json:"id"`
	ExpiresAt int64       `json:"expiresAt"`
	Location  pb.Location `json:"location"`
}

//
// 3rd party structures API
//

// AllStructures contains the response of https://stop.hammerti.me.uk/api/structure/all.
//easyjson:json
type AllStructures map[string]Structure

// Structure stores an individual structure from AllStructures.
type Structure struct {
	TypeID      int64          `json:"typeId"`
	Name        string         `json:"name"`
	RegionID    int64          `json:"regionId"`
	Coordinates pb.Coordinates `json:"location"`
	TypeName    string         `json:"typeName"`
	SystemID    int64          `json:"systemId"`
	LastSeen    time.Time      `json:"lastSeen"`
	SystemName  string         `json:"systemName"`
	Public      bool           `json:"public"`
	FirstSeen   time.Time      `json:"firstSeen"`
	RegionName  string         `json:"regionName"`
}
