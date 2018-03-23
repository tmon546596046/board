package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"git/inspursoft/board/src/apiserver/service"
	"git/inspursoft/board/src/common/model"
	"git/inspursoft/board/src/common/utils"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/astaxie/beego/logs"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
)

const (
	dockerfileName         = "Dockerfile"
	deploymentFilename     = "deployment.yaml"
	serviceFilename        = "service.yaml"
	rollingUpdateFilename  = "rollingUpdateDeployment.yaml"
	deploymentTestFilename = "testdeployment.yaml"
	serviceTestFilename    = "testservice.yaml"
	serviceProcess         = "process-service"
	rollingUpdate          = "rolling-update"
	apiheader              = "Content-Type: application/yaml"
	deploymentAPI          = "/apis/extensions/v1beta1/namespaces/"
	serviceAPI             = "/api/v1/namespaces/"
	test                   = "test"
	serviceNamespace       = "default" //TODO create in project post
	k8sServices            = "kubernetes"
	deploymentType         = "deployment"
	serviceType            = "service"
	startingDuration       = 300 * time.Second //300 seconds
)

const (
	preparing = iota
	running
	stopped
	uncompleted
	warning
)

type ServiceController struct {
	baseController
}

//  Checking the user priviledge by token
func (p *ServiceController) Prepare() {
	user := p.getCurrentUser()
	if user == nil {
		p.customAbort(http.StatusUnauthorized, "Need to login first.")
		return
	}
	p.currentUser = user
	p.isSysAdmin = (user.SystemAdmin == 1)
}

func (p *ServiceController) generateRepoPathByProject(project *model.Project) string {
	if project == nil {
		p.customAbort(http.StatusBadRequest, "Failed to generate repo path since project is nil.")
	}
	return filepath.Join(baseRepoPath(), p.currentUser.Username, project.Name)
}

func (p *ServiceController) generateRepoPathByProjectName(projectName string) string {
	return filepath.Join(baseRepoPath(), p.currentUser.Username, projectName)
}

func (p *ServiceController) getKey() string {
	return strconv.Itoa(int(p.currentUser.ID))
}

func (p *ServiceController) DeployServiceAction() {
	key := p.getKey()
	configService := NewConfigServiceStep(key)

	isMember, err := service.IsProjectMember(configService.ProjectID, p.currentUser.ID)
	if err != nil {
		p.internalError(err)
		return
	}

	//Judge authority
	if !(p.isSysAdmin || isMember) {
		p.customAbort(http.StatusForbidden, "Insufficient privileges to delete service.")
		return
	}

	var newservice model.ServiceStatus
	newservice.Name = configService.ServiceName
	newservice.ProjectID = configService.ProjectID
	newservice.Status = preparing // 0: preparing 1: running 2: suspending
	newservice.OwnerID = p.currentUser.ID
	newservice.OwnerName = p.currentUser.Username
	newservice.Public = configService.Public

	project, err := service.GetProject(model.Project{ID: configService.ProjectID}, "id")
	if err != nil {
		p.internalError(err)
		return
	}
	if project == nil {
		p.customAbort(http.StatusBadRequest, projectIDInvalidErr.Error())
		return
	}
	newservice.ProjectName = project.Name

	serviceInfo, err := service.CreateServiceConfig(newservice)
	if err != nil {
		p.internalError(err)
		return
	}

	repoPath := p.generateRepoPathByProject(project)
	loadPath := filepath.Join(repoPath, serviceProcess, strconv.Itoa(int(serviceInfo.ID)))
	err = service.CheckDeploymentPath(loadPath)
	if err != nil {
		p.internalError(err)
		return
	}

	err = service.AssembleDeploymentYaml((*model.ConfigServiceStep)(configService), loadPath)
	if err != nil {
		p.internalError(err)
		return
	}

	err = service.AssembleServiceYaml((*model.ConfigServiceStep)(configService), loadPath)
	if err != nil {
		p.internalError(err)
		return
	}

	var pushObject pushObject
	pushObject.UserID = p.currentUser.ID
	pushObject.FileName = fmt.Sprintf("%s,%s", deploymentFilename, serviceFilename)
	pushObject.JobName = serviceProcess
	pushObject.ProjectName = project.Name
	pushObject.Extras = fmt.Sprintf("%s,%s", fmt.Sprintf("%s%s%s/%s", kubeMasterURL(), deploymentAPI, project.Name, "deployments"),
		fmt.Sprintf("%s%s%s/%s", kubeMasterURL(), serviceAPI, project.Name, "services"))
	pushObject.Value = filepath.Join(serviceProcess, strconv.Itoa(int(serviceInfo.ID)))
	pushObject.Message = fmt.Sprintf("Create service for project %s with service %d", project.Name, serviceInfo.ID)

	relPath := filepath.Join(serviceProcess, strconv.Itoa(int(serviceInfo.ID)))

	generateMetaConfiguration(&pushObject, repoPath)
	pushObject.Items = []string{"META.cfg", filepath.Join(relPath, deploymentFilename), filepath.Join(relPath, serviceFilename)}

	ret, msg, err := InternalPushObjects(&pushObject, &(p.baseController))
	if err != nil {
		p.internalError(err)
		return
	}
	logs.Info("Internal push deployment object: %d %s", ret, msg)

	serviceConfig, err := json.Marshal(&configService)
	if err != nil {
		p.internalError(err)
		return
	}

	updateService := model.ServiceStatus{ID: serviceInfo.ID, Status: running, ServiceConfig: string(serviceConfig)}
	_, err = service.UpdateService(updateService, "id", "status", "service_config")
	if err != nil {
		p.internalError(err)
		return
	}

	err = DeleteConfigServiceStep(key)
	if err != nil {
		p.internalError(err)
		return
	}
	logs.Info("Service with ID:%d has been deleted in cache.", serviceInfo.ID)

	configService.ServiceID = serviceInfo.ID
	p.Data["json"] = configService
	p.ServeJSON()
}

func (p *ServiceController) DeployServiceTestAction() {
	key := p.getKey()
	configService := NewConfigServiceStep(key)
	configService.ServiceName = test + configService.ServiceName
	SetConfigServiceStep(key, configService)
	p.DeployServiceAction()
}

//
func syncK8sStatus(serviceList []*model.ServiceStatusMO) error {
	var err error
	// synchronize service status with the cluster system
	for _, serviceStatusMO := range serviceList {
		// Get serviceStatus from serviceStatusMO to adapt for updating services
		serviceStatus := &serviceStatusMO.ServiceStatus
		if (*serviceStatus).Status == stopped {
			continue
		}
		// Check the deployment status
		deployment, err := service.GetDeployment((*serviceStatus).ProjectName, (*serviceStatus).Name)
		if deployment == nil && serviceStatus.Name != k8sServices {
			logs.Info("Failed to get deployment", err)
			var reason = "The deployment is not established in cluster system"
			(*serviceStatus).Status = uncompleted
			// TODO create a new field in serviceStatus for reason
			(*serviceStatus).Comment = "Reason: " + reason
			_, err = service.UpdateService(*serviceStatus, "status", "Comment")
			if err != nil {
				logs.Error("Failed to update deployment.")
				break
			}
			continue
		} else {
			if deployment.Status.Replicas > deployment.Status.AvailableReplicas {
				logs.Debug("The desired replicas number is not available",
					deployment.Status.Replicas, deployment.Status.AvailableReplicas)
				(*serviceStatus).Status = uncompleted
				reason := "The desired replicas number is not available"
				(*serviceStatus).Comment = "Reason: " + reason
				_, err = service.UpdateService(*serviceStatus, "status", "Comment")
				if err != nil {
					logs.Error("Failed to update deployment replicas.")
					break
				}
				continue
			}
		}

		// Check the service in k8s cluster status
		serviceK8s, err := service.GetK8sService((*serviceStatus).ProjectName, (*serviceStatus).Name)
		if serviceK8s == nil {
			logs.Info("Failed to get service in cluster", err)
			var reason = "The service is not established in cluster system"
			(*serviceStatus).Status = uncompleted
			(*serviceStatus).Comment = "Reason: " + reason
			_, err = service.UpdateService(*serviceStatus, "status", "Comment")
			if err != nil {
				logs.Error("Failed to update service in cluster.")
				break
			}
			continue
		}

		if serviceStatus.Status == uncompleted {
			logs.Info("The service is restored to running")
			(*serviceStatus).Status = running
			(*serviceStatus).Comment = ""
			_, err = service.UpdateService(*serviceStatus, "status", "Comment")
			if err != nil {
				logs.Error("Failed to update service status.")
				break
			}
			continue
		}
	}
	return err
}

//get service list
func (p *ServiceController) GetServiceListAction() {
	serviceName := p.GetString("service_name", "")
	pageIndex, _ := p.GetInt("page_index", 0)
	pageSize, _ := p.GetInt("page_size", 0)
	orderField := p.GetString("order_field", "CREATE_TIME")
	orderAsc, _ := p.GetInt("order_asc", 0)
	if pageIndex == 0 && pageSize == 0 {
		serviceStatus, err := service.GetServiceList(serviceName, p.currentUser.ID)
		if err != nil {
			p.internalError(err)
			return
		}
		err = syncK8sStatus(serviceStatus)
		if err != nil {
			p.internalError(err)
			return
		}
		p.Data["json"] = serviceStatus
	} else {
		paginatedServiceStatus, err := service.GetPaginatedServiceList(serviceName, p.currentUser.ID, pageIndex, pageSize, orderField, orderAsc)
		if err != nil {
			p.internalError(err)
			return
		}
		err = syncK8sStatus(paginatedServiceStatus.ServiceStatusList)
		if err != nil {
			p.internalError(err)
			return
		}
		p.Data["json"] = paginatedServiceStatus
	}
	p.ServeJSON()
}

// API to create service config
func (p *ServiceController) CreateServiceConfigAction() {
	reqData, err := p.resolveBody()
	if err != nil {
		p.internalError(err)
		return
	}
	var reqServiceProject model.ServiceProject
	err = json.Unmarshal(reqData, &reqServiceProject)
	if err != nil {
		p.internalError(err)
		return
	}
	//Assign and return Service ID with mysql
	var newservice model.ServiceStatus
	newservice.ProjectID = reqServiceProject.ProjectID
	newservice.ProjectName = reqServiceProject.ProjectName
	newservice.Status = preparing // 0: preparing 1: running 2: suspending
	newservice.OwnerID = p.currentUser.ID

	isMember, err := service.IsProjectMember(newservice.ProjectID, p.currentUser.ID)
	if err != nil {
		p.internalError(err)
		return
	}

	//Judge authority
	if !(p.isSysAdmin || isMember) {
		p.customAbort(http.StatusForbidden, "Insufficient privileges to create service.")
		return
	}

	serviceInfo, err := service.CreateServiceConfig(newservice)
	if err != nil {
		p.internalError(err)
		return
	}
	p.Data["json"] = strconv.Itoa(int(serviceInfo.ID))
	p.ServeJSON()
}

func cleanDeploymentK8s(s *model.ServiceStatus) error {
	logs.Info("clean in cluster %s", s.Name)
	// Stop deployment
	cli, err := service.K8sCliFactory("", kubeMasterURL(), "v1beta1")
	apiSet, err := kubernetes.NewForConfig(cli)
	if err != nil {
		return err
	}
	d := apiSet.Deployments(s.ProjectName)
	deployData, err := d.Get(s.Name)
	if err != nil {
		logs.Debug("Do not need to clean deployment")
		return nil
	}

	var newreplicas int32
	deployData.Spec.Replicas = &newreplicas
	res, err := d.Update(deployData)
	if err != nil {
		logs.Error(res, err)
		return err
	}
	time.Sleep(2)
	err = d.Delete(s.Name, nil)
	if err != nil {
		logs.Error("Failed to delele deployment", s.Name, err)
		return err
	}
	logs.Info("Deleted deployment %s", s.Name)

	r := apiSet.ReplicaSets(s.ProjectName)
	var listoption v1.ListOptions
	listoption.LabelSelector = "app=" + s.Name
	rsList, err := r.List(listoption)
	if err != nil {
		logs.Error("failed to get rs list")
		return err
	}

	for _, rsi := range rsList.Items {
		err = r.Delete(rsi.Name, nil)
		if err != nil {
			logs.Error("failed to delete rs %s", rsi.Name)
			return err
		}
		logs.Debug("delete RS %s", rsi.Name)
	}

	return nil
}

func cleanServiceK8s(s *model.ServiceStatus) error {
	logs.Info("clean Service in cluster %s", s.Name)
	//Stop service in cluster
	cli, err := service.K8sCliFactory("", kubeMasterURL(), "v1")
	apiSet, err := kubernetes.NewForConfig(cli)
	if err != nil {
		return err
	}
	servcieInt := apiSet.Services(s.ProjectName)
	_, err = servcieInt.Get(s.Name)
	if err != nil {
		logs.Debug("Do not need to clean service %s", s.Name)
		return nil
	}
	err = servcieInt.Delete(s.Name, nil)
	if err != nil {
		logs.Error("Failed to delele service in cluster.", s.Name, err)
		return err
	}

	return nil
}

func (p *ServiceController) DeleteServiceAction() {
	serviceID, err := strconv.ParseInt(p.Ctx.Input.Param(":id"), 10, 64)
	if err != nil {
		p.internalError(err)
		return
	}
	// Check service id exist
	var servicequery model.ServiceStatus
	servicequery.ID = int64(serviceID)
	s, err := service.GetService(servicequery, "id")
	if err != nil {
		p.internalError(err)
		return
	}
	if s == nil {
		p.customAbort(http.StatusBadRequest, fmt.Sprintf("Invalid service ID: %d", serviceID))
		return
	}

	isMember, err := service.IsProjectMember(s.ProjectID, p.currentUser.ID)
	if err != nil {
		p.internalError(err)
		return
	}

	//Judge authority
	if !(p.isSysAdmin || isMember) {
		p.customAbort(http.StatusForbidden, "Insufficient privileges to delete service.")
		return
	}

	// Call stop service if running
	switch s.Status {
	case running:
		//err = stopService(s)
		err = stopServiceK8s(s)
		if err != nil {
			p.internalError(err)
			return
		}
	case uncompleted:
		timeInt := time.Now().Sub(s.UpdateTime)
		logs.Debug("uncompleted status in %+v", timeInt)
		if timeInt < startingDuration {
			p.customAbort(http.StatusBadRequest,
				fmt.Sprintf("Invalid request %d in starting status", serviceID))
			return
		}
		err = cleanDeploymentK8s(s)
		if err != nil {
			logs.Error("Failed to clean deployment %s", s.Name)
			p.internalError(err)
			return
		}
		err = cleanServiceK8s(s)
		if err != nil {
			logs.Error("Failed to clean service %s", s.Name)
			p.internalError(err)
			return
		}
	}

	isSuccess, err := service.DeleteService(serviceID)
	if err != nil {
		p.internalError(err)
		return
	}
	if !isSuccess {
		p.customAbort(http.StatusBadRequest, fmt.Sprintf("Failed to delete service with ID: %d", serviceID))
	}

	//delete repo files of the service
	repoPath := p.generateRepoPathByProjectName(s.ProjectName)
	var serviceFiles pushObject
	serviceFiles.Items = []string{filepath.Join(repoPath, serviceProcess, strconv.Itoa(int(serviceID)), serviceFilename),
		filepath.Join(repoPath, serviceProcess, strconv.Itoa(int(serviceID)), deploymentFilename)}
	serviceFiles.Message = fmt.Sprintf("Delete yaml files for project %s service %d", s.ProjectName, serviceID)
	ret, msg, err := InternalCleanObjects(&serviceFiles, &(p.baseController))
	if err != nil {
		p.internalError(err)
		return
	}
	logs.Info("Internal push deployment object: %d %s", ret, msg)
}

// API to deploy service
func (p *ServiceController) ToggleServiceAction() {
	serviceID, err := strconv.Atoi(p.Ctx.Input.Param(":id"))
	if err != nil {
		p.internalError(err)
		return
	}

	reqData, err := p.resolveBody()
	if err != nil {
		p.internalError(err)
		return
	}
	var reqServiceToggle model.ServiceToggle
	err = json.Unmarshal(reqData, &reqServiceToggle)
	if err != nil {
		p.internalError(err)
		return
	}
	logs.Info(reqServiceToggle)

	// Check the current service status
	var servicequery model.ServiceStatus
	servicequery.ID = int64(serviceID)
	s, err := service.GetService(servicequery, "id")
	if err != nil {
		p.internalError(err)
		return
	}
	if s == nil {
		p.customAbort(http.StatusBadRequest, fmt.Sprintf("Invalid service ID: %d", serviceID))
		return
	}

	isMember, err := service.IsProjectMember(s.ProjectID, p.currentUser.ID)
	if err != nil {
		p.internalError(err)
		return
	}

	//Judge authority
	if !(p.isSysAdmin || isMember) {
		p.customAbort(http.StatusForbidden, "Insufficient privileges to toggle service status.")
		return
	}

	if s.Status == stopped && reqServiceToggle.Toggle == 0 {
		p.customAbort(http.StatusBadRequest, "Service already stopped.")
		return
	}

	if s.Status == running && reqServiceToggle.Toggle == 1 {
		p.customAbort(http.StatusBadRequest, "Service already running.")
		return
	}

	if reqServiceToggle.Toggle == 0 {
		// stop service
		//err = stopService(s)
		err = stopServiceK8s(s)
		if err != nil {
			p.internalError(err)
			return
		}
		//logs.Info("Stop service successful")
		// Update service status DB
		servicequery.Status = stopped
		_, err = service.UpdateService(servicequery, "status")
		if err != nil {
			p.internalError(err)
			return
		}
	} else {
		// start service
		//serviceNamespace = reqServiceConfig.ProjectName TODO in project
		// Push deployment to jenkins
		var pushobject pushObject
		pushobject.FileName = deploymentFilename
		pushobject.JobName = serviceProcess
		pushobject.Value = filepath.Join(serviceProcess, strconv.Itoa(serviceID))
		pushobject.ProjectName = s.ProjectName

		pushobject.Message = fmt.Sprintf("Create deployment for project %s service %d",
			s.ProjectName, s.ID)
		pushobject.Extras = filepath.Join(kubeMasterURL(), deploymentAPI,
			s.ProjectName, "deployments")

		// Add deployment file
		pushobject.Items = []string{filepath.Join(pushobject.Value, deploymentFilename)}

		ret, msg, err := InternalPushObjects(&pushobject, &(p.baseController))
		if err != nil {
			p.internalError(err)
			return
		}
		logs.Info("Internal push deployment object: %d %s", ret, msg)

		//TODO: If fail to create deployment, should not continue to create service

		//Push service to jenkins
		pushobject.FileName = serviceFilename
		pushobject.Message = fmt.Sprintf("Create service for project %s service %d",
			s.ProjectName, s.ID)
		pushobject.Extras = filepath.Join(kubeMasterURL(), serviceAPI, s.ProjectName, "services")
		// Add deployment file
		pushobject.Items = []string{filepath.Join(pushobject.Value, serviceFilename)}

		ret, msg, err = InternalPushObjects(&pushobject, &(p.baseController))
		if err != nil {
			p.internalError(err)
			return
		}
		logs.Debug("Internal push service object: %d %s", ret, msg)

		// Update service status DB
		servicequery.Status = running
		_, err = service.UpdateService(servicequery, "status")
		if err != nil {
			p.internalError(err)
			return
		}
		//logs.Info("Start service successful")
	}
}

func stopService(s *model.ServiceStatus) error {
	var err error
	var client = &http.Client{}
	// Stop service
	//deleteServiceURL := filepath.Join(kubeMasterURL(), serviceAPI,
	//	serviceNamespace, "services", s.Name)
	deleteServiceURL := kubeMasterURL() + serviceAPI + s.ProjectName + "/services/" + s.Name
	req, err := http.NewRequest("DELETE", deleteServiceURL, nil)
	if err != nil {
		logs.Error("Failed to new request for delete service: %s", deleteServiceURL)
		return err
	}
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := client.Do(req)
	if err != nil {
		logs.Info(req)
		return err
	}
	defer resp.Body.Close()
	logs.Info("Stop service successfully", s.ID, s.Name, resp)

	// Stop deployment
	//deleteDeploymentURL := filepath.Join(kubeMasterURL(), deploymentAPI,
	//	serviceNamespace, "deployments", s.Name)
	deleteDeploymentURL := kubeMasterURL() + deploymentAPI + s.ProjectName + "/deployments/" + s.Name
	req, err = http.NewRequest("DELETE", deleteDeploymentURL, nil)
	if err != nil {
		logs.Error("Failed to new request for delete deployment: %s", deleteDeploymentURL)
		return err
	}
	req.Header.Set("Content-Type", "application/yaml")
	resp, err = client.Do(req)
	if err != nil {
		logs.Error(req)
		return err
	}
	defer resp.Body.Close()

	logs.Info("Stop deployment successfully, id: %d, name: %s, resp: %+v", s.ID, s.Name, resp)
	return nil
}

func stopServiceK8s(s *model.ServiceStatus) error {
	logs.Info("stop service in cluster %s", s.Name)
	// Stop deployment
	cli, err := service.K8sCliFactory("", kubeMasterURL(), "v1beta1")
	apiSet, err := kubernetes.NewForConfig(cli)
	if err != nil {
		return err
	}
	d := apiSet.Deployments(s.ProjectName)
	deployData, err := d.Get(s.Name)
	if err != nil {
		logs.Error("Failed to get deployment in cluster")
		return err
	}

	var newreplicas int32
	deployData.Spec.Replicas = &newreplicas
	res, err := d.Update(deployData)
	if err != nil {
		logs.Error(res, err)
		return err
	}
	time.Sleep(2)
	err = d.Delete(s.Name, nil)
	if err != nil {
		logs.Error("Failed to delele deployment", s.Name, err)
		return err
	}
	logs.Info("Deleted deployment %s", s.Name)

	r := apiSet.ReplicaSets(s.ProjectName)
	var listoption v1.ListOptions
	listoption.LabelSelector = "app=" + s.Name
	rsList, err := r.List(listoption)
	if err != nil {
		logs.Error("failed to get rs list")
		return err
	}

	for _, rsi := range rsList.Items {
		err = r.Delete(rsi.Name, nil)
		if err != nil {
			logs.Error("failed to delete rs %s", rsi.Name)
			return err
		}
		logs.Debug("delete RS %s", rsi.Name)
	}

	//Stop service in cluster
	cli, err = service.K8sCliFactory("", kubeMasterURL(), "v1")
	apiSet, err = kubernetes.NewForConfig(cli)
	if err != nil {
		return err
	}
	servcieInt := apiSet.Services(s.ProjectName)
	//serviceData, err := servcieInt.Get(s.Name)
	//if err != nil {
	//	logs.Error("Failed to get service in cluster %s", s.Name)
	//	return err
	//}
	err = servcieInt.Delete(s.Name, nil)
	if err != nil {
		logs.Error("Failed to delele service in cluster.", s.Name, err)
		return err
	}

	return nil
}

func (p *ServiceController) resolveErrOutput(err error) {
	if err != nil {
		if strings.Index(err.Error(), "StatusNotFound:") == 0 {
			var output interface{}
			json.Unmarshal([]byte(err.Error()[len("StatusNotFound:"):]), &output)
			p.Data["json"] = output
			p.ServeJSON()
			return
		}
		p.internalError(err)
	}
}

func (p *ServiceController) GetServiceInfoAction() {
	var serviceInfo model.ServiceInfoStruct

	//Get Nodeport
	serviceID, err := strconv.Atoi(p.Ctx.Input.Param(":id"))
	if err != nil {
		p.internalError(err)
		return
	}

	var servicequery model.ServiceStatus
	servicequery.ID = int64(serviceID)
	s, err := service.GetService(servicequery, "id")
	if err != nil {
		p.internalError(err)
		return
	}
	if s == nil {
		p.customAbort(http.StatusBadRequest, fmt.Sprintf("Invalid service ID: %d", serviceID))
		return
	}

	isMember, err := service.IsProjectMember(s.ProjectID, p.currentUser.ID)
	if err != nil {
		p.internalError(err)
		return
	}

	//Judge authority
	if !(p.isSysAdmin || isMember) {
		p.customAbort(http.StatusForbidden, "Insufficient privileges to get publicity of service.")
		return
	}
	serviceStatus, err := service.GetServiceStatus(kubeMasterURL() + serviceAPI + s.ProjectName + "/services/" + s.Name)
	if err != nil {
		p.resolveErrOutput(err)
		return
	}
	//Get NodeIP
	//endpointUrl format /api/v1/namespaces/default/endpoints/
	nodesStatus, err := service.GetNodesStatus(fmt.Sprintf("%s/api/v1/nodes", kubeMasterURL()))
	if err != nil {
		p.resolveErrOutput(err)
		return
	}
	if len(serviceStatus.Spec.Ports) == 0 || len(nodesStatus.Items) == 0 {
		p.Data["json"] = "NA"
		p.ServeJSON()
		return
	}

	for _, ports := range serviceStatus.Spec.Ports {
		serviceInfo.NodePort = append(serviceInfo.NodePort, ports.NodePort)
	}
	for _, items := range nodesStatus.Items {
		serviceInfo.NodeName = append(serviceInfo.NodeName, items.Status.Addresses...)
	}

	p.Data["json"] = serviceInfo
	p.ServeJSON()
}

func (p *ServiceController) GetServiceStatusAction() {
	serviceID, err := strconv.Atoi(p.Ctx.Input.Param(":id"))
	if err != nil {
		p.internalError(err)
		return
	}

	var servicequery model.ServiceStatus
	servicequery.ID = int64(serviceID)
	s, err := service.GetService(servicequery, "id")
	if err != nil {
		p.internalError(err)
		return
	}
	if s == nil {
		p.customAbort(http.StatusBadRequest, fmt.Sprintf("Invalid service ID: %d", serviceID))
		return
	}

	isMember, err := service.IsProjectMember(s.ProjectID, p.currentUser.ID)
	if err != nil {
		p.internalError(err)
		return
	}

	//Judge authority
	if !(p.isSysAdmin || isMember) {
		p.customAbort(http.StatusForbidden, "Insufficient privileges to get publicity of service.")
		return
	}

	serviceStatus, err := service.GetServiceStatus(kubeMasterURL() + serviceAPI + s.ProjectName + "/services/" + s.Name)
	if err != nil {
		p.resolveErrOutput(err)
		return
	}
	p.Data["json"] = serviceStatus
	p.ServeJSON()
}

func (p *ServiceController) ServicePublicityAction() {
	serviceID, err := strconv.Atoi(p.Ctx.Input.Param(":id"))
	if err != nil {
		p.internalError(err)
		return
	}

	reqData, err := p.resolveBody()
	if err != nil {
		p.internalError(err)
		return
	}
	var reqServiceUpdate model.ServicePublicityUpdate
	err = json.Unmarshal(reqData, &reqServiceUpdate)
	if err != nil {
		p.internalError(err)
		return
	}
	logs.Info(reqServiceUpdate)

	// Check the current service status
	var servicequery model.ServiceStatus
	servicequery.ID = int64(serviceID)
	s, err := service.GetService(servicequery, "id")
	if err != nil {
		p.internalError(err)
		return
	}
	if s == nil {
		p.customAbort(http.StatusBadRequest, fmt.Sprintf("Invalid service ID: %d", serviceID))
		return
	}

	isMember, err := service.IsProjectMember(s.ProjectID, p.currentUser.ID)
	if err != nil {
		p.internalError(err)
		return
	}

	//Judge authority
	if !(p.isSysAdmin || isMember) {
		p.customAbort(http.StatusForbidden, "Insufficient privileges to get publicity of service.")
		return
	}

	if s.Public != reqServiceUpdate.Public {
		servicequery.Public = reqServiceUpdate.Public
		_, err = service.UpdateService(servicequery, "public")
		if err != nil {
			p.internalError(err)
			return
		}
	} else {
		logs.Info("Already in target publicity status")
	}
}

func (p *ServiceController) DeleteServiceConfigAction() {
	var err error
	serviceID, err := strconv.Atoi(p.Ctx.Input.Param(":id"))
	if err != nil {
		p.internalError(err)
		return
	}

	// Get the project info of this service
	var servicequery model.ServiceStatus
	servicequery.ID = int64(serviceID)
	s, err := service.GetService(servicequery, "id")
	if err != nil {
		p.internalError(err)
		return
	}
	if s == nil {
		p.customAbort(http.StatusBadRequest,
			fmt.Sprintf("Invalid service ID: %d", serviceID))
		return
	}
	logs.Info("service status: ", s)

	// Get the path of the service config files
	repoPath := p.generateRepoPathByProjectName(s.ProjectName)
	serviceConfigPath := filepath.Join(repoPath, serviceProcess, strconv.Itoa(serviceID))
	logs.Debug("Service config path: %s", serviceConfigPath)

	// Delete yaml files
	// TODO
	err = service.DeleteServiceConfigYaml(serviceConfigPath)
	if err != nil {
		logs.Info("failed to delete service yaml", serviceConfigPath)
		p.internalError(err)
		return
	}

	// For terminated service config, actually delete it in DB
	_, err = service.DeleteServiceByID(servicequery)
	if err != nil {
		p.internalError(err)
		return
	}
}

func (p *ServiceController) DeleteDeploymentAction() {
	var err error
	serviceID, err := strconv.Atoi(p.Ctx.Input.Param(":id"))
	if err != nil {
		p.internalError(err)
		return
	}

	// Get the project info of this service
	var servicequery model.ServiceStatus
	servicequery.ID = int64(serviceID)
	s, err := service.GetService(servicequery, "id")
	if err != nil {
		p.internalError(err)
		return
	}
	if s == nil {
		p.customAbort(http.StatusBadRequest,
			fmt.Sprintf("Invalid service ID: %d", serviceID))
		return
	}
	logs.Info("service status: ", s)

	// Get the path of the service config files
	repoPath := p.generateRepoPathByProjectName(s.ProjectName)
	serviceConfigPath := filepath.Join(repoPath, serviceProcess, strconv.Itoa(serviceID))
	logs.Debug("Service config path: %s", serviceConfigPath)

	// TODO clear kube-master, even if the service is not deployed successfully

	// Update git repo
	var pushobject pushObject
	pushobject.FileName = deploymentFilename
	pushobject.JobName = serviceProcess
	pushobject.ProjectName = s.ProjectName
	pushobject.Value = filepath.Join(serviceProcess, strconv.Itoa(serviceID))

	pushobject.Message = fmt.Sprintf("Delete yaml files for project %s service %d",
		s.ProjectName, s.ID)
	pushobject.Extras = filepath.Join(kubeMasterURL(), deploymentAPI,
		s.ProjectName, "deployments")

	//Get file list for Jenkis git repo
	uploads, err := service.ListUploadFiles(serviceConfigPath)
	if err != nil {
		p.internalError(err)
		return
	}
	// Add yaml files
	for _, finfo := range uploads {
		filefullname := filepath.Join(pushobject.Value, finfo.FileName)
		pushobject.Items = append(pushobject.Items, filefullname)
	}

	ret, msg, err := InternalCleanObjects(&pushobject, &(p.baseController))
	if err != nil {
		logs.Info("Failed to push object for git repo clean", msg, ret, pushobject)
		p.internalError(err)
		return
	}
	logs.Info("Internal push clean deployment object: %d %s", ret, msg)

	// Delete yaml files
	err = service.DeleteServiceConfigYaml(serviceConfigPath)
	if err != nil {
		logs.Info("failed to delete service yaml", serviceConfigPath)
		p.internalError(err)
		return
	}

	// For terminated service config, actually delete it in DB
	_, err = service.DeleteServiceByID(servicequery)
	if err != nil {
		p.internalError(err)
		return
	}
}

func (p *ServiceController) StoreServiceRoute() {
	serviceIdentity := p.GetString("service_identity")
	serviceURL := p.GetString("service_url")
	memoryCache.Put(strings.ToLower(serviceIdentity), serviceURL, time.Second*time.Duration(tokenCacheExpireSeconds))
	logs.Debug("Service identity: %s, URL: %s", serviceIdentity, serviceURL)
}

func (p *ServiceController) ServiceExists() {
	projectName := p.GetString("project_name")
	serviceName := p.GetString("service_name")
	isServiceExists, err := service.ServiceExists(serviceName, projectName)
	if err != nil {
		p.internalError(err)
		logs.Error("Check service name failed, error: %+v", err.Error())
		return
	}
	if isServiceExists == true {
		p.customAbort(http.StatusConflict, serverNameDuplicateErr.Error())
		return
	}
}

func (p *ServiceController) ScaleServiceAction() {
	serviceID, err := strconv.Atoi(p.Ctx.Input.Param(":id"))
	if err != nil {
		p.internalError(err)
		return
	}

	reqData, err := p.resolveBody()
	if err != nil {
		p.internalError(err)
		return
	}
	var reqServiceScale model.ServiceScale
	err = json.Unmarshal(reqData, &reqServiceScale)
	if err != nil {
		p.internalError(err)
		return
	}
	logs.Info(reqServiceScale)

	// Get the current service status
	var servicequery model.ServiceStatus
	servicequery.ID = int64(serviceID)
	s, err := service.GetService(servicequery, "id")
	if err != nil {
		p.internalError(err)
		return
	}
	if s == nil {
		p.customAbort(http.StatusBadRequest, fmt.Sprintf("Invalid service ID: %d", serviceID))
		return
	}

	isMember, err := service.IsProjectMember(s.ProjectID, p.currentUser.ID)
	if err != nil {
		p.internalError(err)
		return
	}

	//Judge authority
	if !(p.isSysAdmin || isMember) {
		p.customAbort(http.StatusForbidden, "Insufficient privileges to get publicity of service.")
		return
	}

	// change the replica number of service

	res, err := service.ScaleReplica(*s, reqServiceScale.Replica)

	if res != true {
		logs.Info("Failed to scale service replica", s, reqServiceScale.Replica)
		p.internalError(err)
		return
	}
	logs.Info("Scale service replica successfully")
}

//get selectable service list
func (p *ServiceController) GetSelectableServicesAction() {
	serviceName := p.GetString("service_name", "")
	projectName := p.GetString("project_name", "")
	logs.Info("Get selectable service list for", projectName, serviceName)
	serviceList, err := service.GetSelectableServices(projectName, serviceName)
	if err != nil {
		logs.Error("Failed to get selectable services.")
		p.internalError(err)
		return
	}
	p.Data["json"] = serviceList
	p.ServeJSON()
}

func (f *ServiceController) resolveUploadedYamlFile(uploadedFileName string, target interface{}, customError error) func(fileName string, serviceInfo *model.ServiceStatus) error {
	uploadedFile, _, err := f.GetFile(uploadedFileName)
	if err != nil {
		if err.Error() == "http: no such file" {
			f.customAbort(http.StatusBadRequest, "Missing file: "+uploadedFileName)
		}
		f.internalError(err)
	}
	err = utils.UnmarshalYamlFile(uploadedFile, target)
	if err != nil {
		if strings.Index(err.Error(), "InternalError:") == 0 {
			f.internalError(errors.New(err.Error()[14:]))
		}
		f.customAbort(http.StatusBadRequest, customError.Error())
	}

	return func(fileName string, serviceInfo *model.ServiceStatus) error {
		repoPath := f.generateRepoPathByProjectName(serviceInfo.ProjectName)
		targetFilePath := filepath.Join(repoPath, serviceProcess, strconv.Itoa(int(serviceInfo.ID)))
		err = service.CheckDeploymentPath(targetFilePath)
		if err != nil {
			f.internalError(err)
		}
		return f.SaveToFile(uploadedFileName, filepath.Join(targetFilePath, fileName))
	}
}

func (f *ServiceController) UploadYamlFileAction() {
	projectName := f.GetString("project_name")
	isExistence, err := service.ProjectExists(projectName)
	if err != nil {
		f.internalError(err)
		return
	}
	if isExistence != true {
		f.customAbort(http.StatusBadRequest, "Project doesn't exist.")
		return
	}

	var deploymentConfig service.Deployment
	fhDeployment := f.resolveUploadedYamlFile("deployment_file", &deploymentConfig, service.DeploymentYamlFileUnmarshalErr)

	var serviceConfig service.Service
	fhService := f.resolveUploadedYamlFile("service_file", &serviceConfig, service.ServiceYamlFileUnmarshalErr)

	err = service.CheckDeploymentConfig(projectName, deploymentConfig)
	if err != nil {
		f.customAbort(http.StatusBadRequest, err.Error())
	}
	err = service.CheckServiceConfig(projectName, serviceConfig)
	if err != nil {
		f.customAbort(http.StatusBadRequest, err.Error())
	}

	//check label selector

	serviceInfo, err := service.GetServiceByProject(serviceConfig.Name, projectName)
	if err != nil {
		f.internalError(err)
		return
	}
	if serviceInfo != nil {
		f.customAbort(http.StatusBadRequest, "Service name has been used.")
		return
	}
	serviceInfo, err = service.CreateServiceConfig(model.ServiceStatus{
		Name:        serviceConfig.Name,
		ProjectName: projectName,
		Status:      preparing, // 0: preparing 1: running 2: suspending
		OwnerID:     f.currentUser.ID,
		OwnerName:   f.currentUser.Username,
	})
	if err != nil {
		f.internalError(err)
		return
	}

	err = fhDeployment(deploymentFilename, serviceInfo)
	if err != nil {
		f.internalError(err)
	}
	err = fhService(serviceFilename, serviceInfo)
	if err != nil {
		f.internalError(err)
	}

	f.Data["json"] = serviceInfo
	f.ServeJSON()
}

func (f *ServiceController) DownloadDeploymentYamlFileAction() {
	projectName := f.GetString("project_name")
	isExistence, err := service.ProjectExists(projectName)
	if err != nil {
		f.internalError(err)
		return
	}
	if isExistence != true {
		f.customAbort(http.StatusBadRequest, "Project name is invalid.")
		return
	}

	serviceName := f.GetString("service_name")
	serviceInfo, err := service.GetServiceByProject(serviceName, projectName)
	if err != nil {
		f.internalError(err)
		return
	}
	if serviceInfo == nil {
		f.customAbort(http.StatusBadRequest, "Service name is invalid.")
		return
	}

	//get paras
	yamlType := f.GetString("yaml_type")
	fileName := getYamlFileName(yamlType)
	if fileName == "" {
		f.customAbort(http.StatusBadRequest, "Yaml type is invalid.")
		return
	}

	repoPath := f.generateRepoPathByProjectName(projectName)
	absFileName := filepath.Join(repoPath, serviceProcess, strconv.Itoa(int(serviceInfo.ID)), fileName)
	logs.Info("User: %s download %s yaml file from %s.", f.currentUser.Username, yamlType, absFileName)

	//check doc isexist
	if _, err := os.Stat(absFileName); os.IsNotExist(err) {
		//generate file
		err = service.CheckDeploymentPath(filepath.Dir(absFileName))
		if err != nil {
			f.internalError(err)
			return
		}
		//if no doc, get config from k8s; generate yaml file;
		if yamlType == deploymentType {
			deployConfigURL := fmt.Sprintf("%s%s", kubeMasterURL(), filepath.Join(deploymentAPI, projectName, "deployments", serviceName))
			logs.Info("deployConfigURL:", deployConfigURL)
			err := service.GenerateDeploymentYamlFileFromK8S(deployConfigURL, absFileName)
			if err != nil {
				if strings.Index(err.Error(), "StatusNotFound:") == 0 {
					f.customAbort(http.StatusNotFound, service.DeploymentNotFoundErr.Error())
					return
				}
				f.internalError(err)
				return
			}
		} else if yamlType == serviceType {
			serviceConfigURL := fmt.Sprintf("%s%s", kubeMasterURL(), filepath.Join(serviceAPI, projectName, "services", serviceName))
			logs.Info("serviceConfigURL:", serviceConfigURL)
			err := service.GenerateServiceYamlFileFromK8S(serviceConfigURL, absFileName)
			if err != nil {
				if strings.Index(err.Error(), "StatusNotFound:") == 0 {
					f.customAbort(http.StatusNotFound, service.ServiceNotFoundErr.Error())
					return
				}
				f.internalError(err)
				return
			}
		}
	}

	f.Ctx.Output.Download(absFileName, fileName)
}

func getYamlFileName(yamlType string) string {
	var fileName string
	if yamlType == deploymentType {
		fileName = deploymentFilename
	} else if yamlType == serviceType {
		fileName = serviceFilename
	} else {
		return ""
	}
	return fileName
}

func (p *ServiceController) GetScaleStatusAction() {
	serviceID, err := strconv.Atoi(p.Ctx.Input.Param(":id"))
	if err != nil {
		p.internalError(err)
		return
	}
	// Get the current service status
	var servicequery model.ServiceStatus
	servicequery.ID = int64(serviceID)
	s, err := service.GetService(servicequery, "id")
	if err != nil {
		p.internalError(err)
		return
	}
	if s == nil {
		p.customAbort(http.StatusBadRequest, fmt.Sprintf("Invalid service ID: %d", serviceID))
		return
	}

	isMember, err := service.IsProjectMember(s.ProjectID, p.currentUser.ID)
	if err != nil {
		p.internalError(err)
		return
	}

	//Judge authority
	if !(p.isSysAdmin || isMember) {
		p.customAbort(http.StatusForbidden, "Insufficient privileges to get publicity of service.")
		return
	}
	scaleStatus, err := service.GetScaleStatus(s)
	if err != nil {
		logs.Debug("Get scale deployment status failed %s", s.Name)
		p.internalError(err)
		return
	}
	p.Data["json"] = scaleStatus
	p.ServeJSON()
	logs.Info("Get Scale status successfully")
}
