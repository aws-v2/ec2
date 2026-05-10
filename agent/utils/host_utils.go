package utils



// ─── UTIL: GET HOST IP ─────────────────────────────────────────────────────

func getHostIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// ─── UTIL: READ SSH KEY FROM FILE ──────────────────────────────────────────

func readSSHKey() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	keyPath := filepath.Join(home, ".ssh", "serwin_test")

	data, err := os.ReadFile(keyPath)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
