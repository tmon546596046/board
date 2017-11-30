package model

import (
	"time"

	modelK8s "k8s.io/client-go/pkg/api/v1"
)

type ServiceStatus struct {
	ID           int64     `json:"service_id" orm:"column(id)"`
	Name         string    `json:"service_name" orm:"column(name)"`
	ProjectID    int64     `json:"service_project_id" orm:"column(project_id)"`
	ProjectName  string    `json:"service_project_name" orm:"column(project_name)"`
	Comment      string    `json:"service_comment" orm:"column(comment)"`
	OwnerID      int64     `json:"service_owner_id" orm:"column(owner_id)"`
	OwnerName    string    `json:"service_owner_name" orm:"column(owner_name)"`
	Status       int       `json:"service_status" orm:"column(status)"`
	Public       int       `json:"service_public" orm:"column(public)"`
	Deleted      int       `json:"service_deleted" orm:"column(deleted)"`
	CreationTime time.Time `json:"service_creation_time" orm:"column(creation_time)"`
	UpdateTime   time.Time `json:"service_update_time" orm:"column(update_time)"`
}
type ServiceInfoStruct struct {
	NodePort []int32                `json:"node_Port,omitempty"`
	NodeName []modelK8s.NodeAddress `json:"node_Name,omitempty"`
}

//func (s *Service) TableName() string {
//	return "service_status"
//}

type ServiceToggle struct {
	Toggle int `json:"service_toggle"`
}

type ServicePublicityUpdate struct {
	Public int `json:"service_public"`
}

type ServiceScale struct {
	Replica int32 `json:"service_scale"`
}

type ExternalService struct {
	ContainerName      string       `json:"container_name"`
	NodeConfig         NodeType     `json:"node_config"`
	LoadBalancerConfig LoadBalancer `json:"load_balancer_config"`
}

type NodeType struct {
	TargetPort int `json:"target_port"`
	NodePort   int `json:"node_port"`
}

type LoadBalancer struct {
	ExternalAccess string `json:"external_access"`
}
