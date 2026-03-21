package interfaces

// SizingMap defines the default resource allocations per size tier.
var SizingMap = map[Size]SizingDefaults{
	SizeXS: {CPU: "0.25", Memory: "512Mi", DBStorage: "10Gi", CacheMemory: "256Mi"},
	SizeS:  {CPU: "1", Memory: "2Gi", DBStorage: "50Gi", CacheMemory: "1Gi"},
	SizeM:  {CPU: "2", Memory: "4Gi", DBStorage: "100Gi", CacheMemory: "4Gi"},
	SizeL:  {CPU: "4", Memory: "16Gi", DBStorage: "500Gi", CacheMemory: "16Gi"},
	SizeXL: {CPU: "8", Memory: "32Gi", DBStorage: "1Ti", CacheMemory: "64Gi"},
}

// SizingDefaults holds the concrete resource values for a given size tier.
type SizingDefaults struct {
	CPU         string `json:"cpu"`
	Memory      string `json:"memory"`
	DBStorage   string `json:"db_storage"`
	CacheMemory string `json:"cache_memory"`
}
