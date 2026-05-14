package host

type NetworkReconcileRequest struct {
    VPCID   string `json:"vpc_id"`
    Bridge  BridgeConfig `json:"bridge"`
    IP      string `json:"ip"`
    Gateway string `json:"gateway"`
}




type BridgeConfig struct{
	Name string `json:"name"`
Gateway string `json:"gateway"`
	CIDR string `json:"cidr"`
}