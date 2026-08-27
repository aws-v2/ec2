package transport

import (
	"net/http"
	"strconv"

	"ec2-api/internal/application"
	dto "ec2-api/internal/domain/dto"
	domain "ec2-api/internal/domain/instance"

	"github.com/gin-gonic/gin"
)

type NetworkingHandler struct {
	service *application.NetworkingService
}

func NewNetworkingHandler(service *application.NetworkingService) *NetworkingHandler {
	return &NetworkingHandler{service: service}
}

// IP Handlers
func (h *NetworkingHandler) AllocateIP(c *gin.Context) {
	var req domain.AllocateIPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Optional body
		req = domain.AllocateIPRequest{}
	}

	ip, err := h.service.AllocateIP(&req)
	if err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusAccepted, "IP allocation successful", ip)
}

func (h *NetworkingHandler) ListIPs(c *gin.Context) {
	ips, err := h.service.ListIPs()
	if err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if ips == nil {
		ips = []*domain.IPAllocation{}
	}

	dto.SendSuccess(c, http.StatusOK, "IP allocations retrieved successfully", ips)
}

func (h *NetworkingHandler) ReleaseIP(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		dto.SendError(c, http.StatusBadRequest, "invalid ip id")
		return
	}

	if err := h.service.ReleaseIP(id); err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "IP allocation released", nil)
}

// Security Group Handlers
func (h *NetworkingHandler) CreateSecurityGroup(c *gin.Context) {
	var req domain.CreateSecurityGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	sg, err := h.service.CreateSecurityGroup(&req)
	if err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusCreated, "Security group created successfully", sg)
}

func (h *NetworkingHandler) ListSecurityGroups(c *gin.Context) {
	sgs, err := h.service.ListSecurityGroups()
	if err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if sgs == nil {
		sgs = []*domain.SecurityGroup{}
	}

	dto.SendSuccess(c, http.StatusOK, "Security groups retrieved successfully", sgs)
}

func (h *NetworkingHandler) GetSecurityGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		dto.SendError(c, http.StatusBadRequest, "invalid security group id")
		return
	}

	sg, err := h.service.GetSecurityGroup(id)
	if err != nil {
		if err == domain.ErrSecurityGroupNotFound {
			dto.SendError(c, http.StatusNotFound, "security group not found")
			return
		}
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "Security group retrieved successfully", sg)
}

func (h *NetworkingHandler) DeleteSecurityGroup(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		dto.SendError(c, http.StatusBadRequest, "invalid security group id")
		return
	}

	if err := h.service.DeleteSecurityGroup(id); err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "security group deleted", nil)
}

// Rule Handlers
func (h *NetworkingHandler) AddRule(c *gin.Context) {
	idStr := c.Param("id")
	sgID, err := strconv.Atoi(idStr)
	if err != nil {
		dto.SendError(c, http.StatusBadRequest, "invalid security group id")
		return
	}

	var req domain.AddRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	rule, err := h.service.AddRule(sgID, &req)
	if err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusCreated, "Rule added successfully", rule)
}

func (h *NetworkingHandler) RemoveRule(c *gin.Context) {
	idStr := c.Param("id")
	sgID, _ := strconv.Atoi(idStr)
	ruleIdStr := c.Param("ruleId")
	ruleID, err := strconv.Atoi(ruleIdStr)
	if err != nil {
		dto.SendError(c, http.StatusBadRequest, "invalid rule id")
		return
	}

	if err := h.service.RemoveRule(sgID, ruleID); err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "Rule removed successfully", nil)
}

// Assignment Handlers
func (h *NetworkingHandler) AssignToInstance(c *gin.Context) {
	instanceID := c.Param("id")
	var req struct {
		SecurityGroupID int `json:"security_group_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.AssignToInstance(instanceID, req.SecurityGroupID); err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "Security group assigned to instance", nil)
}

func (h *NetworkingHandler) RemoveFromInstance(c *gin.Context) {
	instanceID := c.Param("id")
	sgIdStr := c.Param("sgId")
	sgID, err := strconv.Atoi(sgIdStr)
	if err != nil {
		dto.SendError(c, http.StatusBadRequest, "invalid security group id")
		return
	}

	if err := h.service.RemoveFromInstance(instanceID, sgID); err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "Security group removed from instance", nil)
}

func (h *NetworkingHandler) ListForInstance(c *gin.Context) {
	instanceID := c.Param("id")
	sgs, err := h.service.ListForInstance(instanceID)
	if err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if sgs == nil {
		sgs = []*domain.SecurityGroup{}
	}

	dto.SendSuccess(c, http.StatusOK, "Security groups for instance retrieved successfully!!", sgs)
}

func (h *NetworkingHandler) ListVPCs(c *gin.Context) {
	tenantID := ""
	if val, ok := c.Get("userID"); ok {
		tenantID = val.(string)
	}

	vpcs, err := h.service.ListVPCs(c.Request.Context(), tenantID)
	if err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if vpcs == nil {
		vpcs = []domain.VPC{}
	}

	dto.SendSuccess(c, http.StatusOK, "VPCs fetched successfully", vpcs)
}

type CreateVPCPayload struct {
	Name string `json:"name" binding:"required"`
}

func (h *NetworkingHandler) CreateVPC(c *gin.Context) {
	var payload CreateVPCPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		dto.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	tenantID := ""
	if val, ok := c.Get("userID"); ok {
		tenantID = val.(string)
	}

	if err := h.service.CreateVPC(c.Request.Context(), tenantID, payload.Name); err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusAccepted, "VPC creation request submitted successfully", nil)
}

func (h *NetworkingHandler) AssignVPC(c *gin.Context) {
	instanceID := c.Param("id")
	tenantID := ""
	if val, ok := c.Get("userID"); ok {
		tenantID = val.(string)
	}

	var req struct {
		VPCID string `json:"vpc_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.SendError(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.AssignVPC(tenantID, instanceID, req.VPCID, map[string]any{}); err != nil {
		dto.SendError(c, http.StatusInternalServerError, err.Error())
		return
	}

	dto.SendSuccess(c, http.StatusOK, "VPC assignment successful", nil)
}
