package config




const TEST_MODE = false





func configFromEnv() Config {
	return Config{
		NodeID:          getEnv("NODE_ID", DefaultNodeID),
		ListenAddr:      getEnv("LISTEN_ADDR", ":9030"),
		ControlPlaneURL: getEnv("CONTROL_PLANE_URL", DefaultControlPlaneURL),
		APIKey:          getEnv("API_KEY", "ak_MWE1ZjQ0NzctNWZjYi00NzJlLTkwMGItYTQ3MTMxNWNhNzhlOmMwYzhmM2JkODBkZTRkZTRhZTliMmNmYWY3NmY1OTky.IZctxGuX1XfHedhx3CsHTZvzOVsC1F4eW6hikn22_8k"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

