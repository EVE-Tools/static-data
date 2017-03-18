package locations

//
// Own types
//

// RequestLocationsBody contains the IDs to be fetched.
type RequestLocationsBody struct {
	Locations []int64 `json:"locationIDs"`
}

// Response contains the locations fetched from cache/backend.
//easyjson:json
type Response map[string]Location

// CachedLocation represents a location chache entry with expiration date.
type CachedLocation struct {
	ID        int64    `json:"id"`
	ExpiresAt int64    `json:"expiresAt"`
	Location  Location `json:"location"`
}

//
// Contains types for dealing with the location APIs
//

// Location returned by this API
type Location struct {
	Station       Station       `json:"station,omitempty"`
	Region        Region        `json:"region,omitempty"`
	SolarSystem   SolarSystem   `json:"solarSystem,omitempty"`
	Constellation Constellation `json:"constellation,omitempty"`
}

// Station contains station info and additional info for structures
type Station struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	TypeID      int64  `json:"typeId,omitempty"`
	TypeName    string `json:"typeName,omitempty"`
	LastSeen    string `json:"lastSeen,omitempty"`
	Public      bool   `json:"public,omitempty"`
	FirstSeen   string `json:"firstSeen,omitempty"`
	Coordinates struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		Z float64 `json:"z"`
	} `json:"position,omitempty"`
}

// Region contains region info
type Region struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// SolarSystem contains solar system info
type SolarSystem struct {
	ID             int64   `json:"id"`
	SecurityStatus float64 `json:"securityStatus"`
	Name           string  `json:"name"`
}

// Constellation contains constellation info
type Constellation struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

//
// 3rd party structures API
//

// AllStructures contains the response of https://stop.hammerti.me.uk/api/structure/all.
//easyjson:json
type AllStructures map[string]Structure

// Structure stores an individual structure from AllStructures.
type Structure struct {
	TypeID      int64  `json:"typeId"`
	Name        string `json:"name"`
	RegionID    int64  `json:"regionId"`
	Coordinates struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		Z float64 `json:"z"`
	} `json:"location"`
	TypeName   string `json:"typeName"`
	SystemID   int64  `json:"systemId"`
	LastSeen   string `json:"lastSeen"`
	SystemName string `json:"systemName"`
	Public     bool   `json:"public"`
	FirstSeen  string `json:"firstSeen"`
	RegionName string `json:"regionName"`
}
