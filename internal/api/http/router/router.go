package transport

import (
	"github.com/gin-gonic/gin"

	transport "ec2-api/internal/api/http/handlers"
	"ec2-api/internal/api/http/middleware"
)

// api/routes.go

func SetupRoutes(r *gin.Engine, 
	instanceHandler *transport.InstanceHandler, 
	volumeHandler *transport.VolumeHandler, 
	snapshotHandler *transport.SnapshotHandler, 
	sshKeyHandler *transport.SSHKeyHandler, 
	networkingHandler *transport.NetworkingHandler, 
	templateHandler *transport.TemplateHandler, 
	terminalHandler *transport.TerminalHandler,
	fleetHandler *transport.FleetHandler,
	docsHandler *transport.DocsHandler) {
	
	// Fleet Console Endpoints
	fleet := r.Group("/api/v1/compute/fleet")
	fleet.Use(middleware.AuthMiddleware())
	{
		fleet.GET("/overview", fleetHandler.GetOverview)
		fleet.GET("/events", fleetHandler.GetEvents)
	}

	// Documentation Endpoints (public, no auth)
	computeDocs := r.Group("/api/v1/compute/docs")
	{
		computeDocs.GET("", docsHandler.GetPublicManifest)
		computeDocs.GET("/:slug", docsHandler.GetPublicDoc)
	}
	
	// Compute API
	compute := r.Group("/api/v1/compute")
	compute.Use(middleware.AuthMiddleware())
	{
		compute.PUT("/instances/:id/vpc", instanceHandler.AssignVPC)
	}

	api := r.Group("/api/v1/ec2")
	api.Use(middleware.AuthMiddleware())
	{
		// Instance CRUD
		api.POST("/instances", instanceHandler.CreateInstance)
		api.GET("/instances", instanceHandler.ListInstances)
		api.GET("/instances/:id", instanceHandler.GetInstance)
		api.GET("/instances/:id/status-checks", instanceHandler.GetStatusChecks)
		api.GET("/instances/:id/metrics", instanceHandler.GetMetrics)
		api.GET("/instances/:id/tags", instanceHandler.GetTags)
		api.POST("/instances/:id/tags", instanceHandler.AddOrUpdateTag)
		api.DELETE("/instances/:id/tags/:key", instanceHandler.DeleteTag)
		api.DELETE("/instances/:id", instanceHandler.DeleteInstance)
		api.PUT("/instances/:id/vpc", networkingHandler.AssignVPC)
		api.GET("/instances/:id/terminal", terminalHandler.HandleTerminal)


		// Instance control
		api.POST("/instances/:id/start", instanceHandler.StartInstance)
		api.POST("/instances/:id/stop", instanceHandler.StopInstance)
		api.POST("/instances/:id/restart", instanceHandler.RestartInstance)

		// Scaling Policy
		api.POST("/scaling-policies", instanceHandler.CreateScalingPolicy)
		api.GET("/scaling-policies", instanceHandler.GetScalingPolicies)
		api.PUT("/scaling-policies/:id", instanceHandler.UpdateScalingPolicy)
		api.DELETE("/scaling-policies/:id", instanceHandler.DeleteScalingPolicy)

		// Snapshots
		api.POST("/instances/:id/snapshot", snapshotHandler.CreateSnapshot)
		api.GET("/snapshots", snapshotHandler.ListSnapshots)
		api.GET("/snapshots/:id", snapshotHandler.GetSnapshot)
		api.DELETE("/snapshots/:id", snapshotHandler.DeleteSnapshot)

		// Templates
		api.POST("/templates", templateHandler.CreateTemplate)
		api.GET("/templates", templateHandler.ListTemplates)
		api.GET("/templates/:id", templateHandler.GetTemplate)
		api.DELETE("/templates/:id", templateHandler.DeleteTemplate)

		// SSH Keys
		api.POST("/ssh-keys", sshKeyHandler.CreateSSHKey)
		api.GET("/ssh-keys", sshKeyHandler.ListSSHKeys)
		api.DELETE("/ssh-keys/:id", sshKeyHandler.DeleteSSHKey)
		api.GET("/ssh-keys/:name/download", sshKeyHandler.DownloadSSHKey)

		// Networking (IP Management)
		api.POST("/ip/allocate", networkingHandler.AllocateIP)
		api.POST("/ip/:id/release", networkingHandler.ReleaseIP)
		api.GET("/ip", networkingHandler.ListIPs)
		
		vpcs := api.Group("/vpcs")
		{
			vpcs.GET("", networkingHandler.ListVPCs)
			vpcs.POST("", networkingHandler.CreateVPC)
			vpcs.PUT("/:id/vpc", networkingHandler.AssignVPC)

		}

		// Security Groups
		api.POST("/security-groups", networkingHandler.CreateSecurityGroup)
		api.GET("/security-groups", networkingHandler.ListSecurityGroups)
		api.GET("/security-groups/:id", networkingHandler.GetSecurityGroup)
		api.DELETE("/security-groups/:id", networkingHandler.DeleteSecurityGroup)
		api.POST("/security-groups/:id/rules", networkingHandler.AddRule)
		api.DELETE("/security-groups/:id/rules/:ruleId", networkingHandler.RemoveRule)

		// Instance Assignment
		api.POST("/instances/:id/security-groups", networkingHandler.AssignToInstance)
		api.DELETE("/instances/:id/security-groups/:sgId", networkingHandler.RemoveFromInstance)
		api.GET("/instances/:id/security-groups", networkingHandler.ListForInstance)


		
		api.POST("/volumes", volumeHandler.CreateVolume)            // Create new block volume
		api.POST("/volumes/:id/reserve", volumeHandler.ReserveVolume)    // Reserve volume record
		api.GET("/volumes", volumeHandler.ListVolumes)              // List all volumes
		api.GET("/volumes/:id", volumeHandler.GetVolume)            // Get volume details
		api.POST("/volumes/:id/attach", volumeHandler.AttachVolume) // Attach volume to instance
		api.POST("/volumes/:id/expand", volumeHandler.ExpandVolume) // Expand volume size
		api.POST("/volumes/:id/detach", volumeHandler.DetachVolume) // Detach volume from instance
		api.POST("/volumes/:id/snapshots", volumeHandler.CreateSnapshot) // Create volume snapshot
		api.GET("/volumes/:id/snapshots", volumeHandler.ListSnapshots)   // List volume snapshots
		api.DELETE("/volumes/:id/snapshot", volumeHandler.DeleteVolumeSnapshot) // Delete volume snapshot
		api.GET("/volumes/:id/tags", volumeHandler.ListTags)             // List volume tags
		api.POST("/volumes/:id/tags", volumeHandler.AddTag)               // Add/Update volume tag
		api.DELETE("/volumes/:id/tags/:key", volumeHandler.DeleteTag)    // Delete volume tag
		api.DELETE("/volumes/:id", volumeHandler.DeleteVolume)      // Delete volume
	}



	// Docs
	docs := api.Group("/docs")
	{
		docs.GET("", docsHandler.GetPublicManifest)
		docs.GET("/:slug", docsHandler.GetPublicDoc)
	}

	internalDocs := api.Group("/internal/docs")
	internalDocs.Use(middleware.AuthMiddleware())
	{
		internalDocs.GET("", docsHandler.GetInternalManifest)
		internalDocs.GET("/:slug", docsHandler.GetInternalDoc)
	}

 
}
