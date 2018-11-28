package controller

import (
	//"encoding/json"
	"fmt"
	"git/inspursoft/board/src/apiserver/service"
	"git/inspursoft/board/src/common/model"
	//"io/ioutil"
	"net/http"
	"strconv"
	//"strings"

	"github.com/astaxie/beego/logs"
)

type PVClaimController struct {
	BaseController
}

func (n *PVClaimController) GetPVClaimAction() {
	//	pvID, err := strconv.Atoi(n.Ctx.Input.Param(":id"))
	//	if err != nil {
	//		n.internalError(err)
	//		return
	//	}
	//	pv, err := service.GetPVDB(model.PersistentVolume{ID: int64(pvID)}, "id")
	//	if err != nil {
	//		n.internalError(err)
	//		return
	//	}
	//	if pv == nil {
	//		logs.Error("Not found this PV %d in DB", pvID)
	//		n.internalError(err)
	//		return
	//	}

	//	// To optimize the different types of common code
	//	switch pv.Type {
	//	case model.PVNFS:
	//		// PV NFS
	//		pvo, err := service.GetPVOptionNFS(model.PersistentVolumeOptionNfs{ID: int64(pvID)}, "id")
	//		if err != nil {
	//			n.internalError(err)
	//			return
	//		}
	//		if pv == nil {
	//			logs.Error("Not found this PV Option %d in DB", pvID)
	//			n.internalError(err)
	//			return
	//		}
	//		pv.Option = pvo

	//	case model.PVCephRBD:
	//		// PV CephRBD
	//		pvo, err := service.GetPVOptionRBD(model.PersistentVolumeOptionCephrbd{ID: int64(pvID)}, "id")
	//		if err != nil {
	//			n.internalError(err)
	//			return
	//		}
	//		if pv == nil {
	//			logs.Error("Not found this PV Option %d in DB", pvID)
	//			n.internalError(err)
	//			return
	//		}
	//		pv.Option = pvo
	//	default:
	//		logs.Error("Unknown pv type %d", pv.Type)
	//		n.customAbort(http.StatusBadRequest, "Unknown pv type")
	//		return
	//	}

	//	// sync the state with K8S

	//	pvk8s, err := service.GetPVK8s(pv.Name)
	//	if err != nil {
	//		logs.Error("Fail to get this PV %s in cluster %v", pv.Name, err)
	//		n.internalError(err)
	//		return
	//	}
	//	if pvk8s == nil {
	//		pv.State = model.InvalidPV
	//	} else {
	//		pv.State = service.ReverseState(string(pvk8s.Status.Phase))
	//	}

	//	n.renderJSON(pv)
	//	logs.Debug("Return get pv %v", pv)
}

func (n *PVClaimController) RemovePVClaimAction() {
	pvcID, err := strconv.Atoi(n.Ctx.Input.Param(":id"))
	if err != nil {
		n.internalError(err)
		return
	}
	pv, err := service.GetPVCDB(model.PersistentVolumeClaimM{ID: int64(pvcID)}, "id")
	if err != nil {
		n.internalError(err)
		return
	}
	if pv == nil {
		logs.Debug("Not found this PVC %d in DB", pvcID)
		return
	}

	//  TODO pvc k8s later
	//	err = service.DeletePVK8s(pv.Name)
	//	if err != nil {
	//		logs.Info("Delete PV %s from K8s Failed %v", pv.Name, err)
	//	} else {
	//		logs.Info("Delete PV %s from K8s Successful %v", pv.Name, err)
	//	}

	// Delete PV DB
	_, err = service.DeletePVCDB(int64(pvcID))
	if err != nil {
		logs.Error("Failed to delete PVC %d", pvcID)
		n.internalError(err)
		return
	}
}

func (n *PVClaimController) GetPVClaimListAction() {
	res, err := service.QueryPVCsByUser(n.currentUser.ID)
	if err != nil {
		logs.Debug("Failed to get PV List")
		n.customAbort(http.StatusInternalServerError, fmt.Sprint(err))
		return
	}
	n.renderJSON(res)
}

func (n *PVClaimController) AddPVClaimAction() {
	var reqPVC model.PersistentVolumeClaimM
	var err error
	err = n.resolveBody(&reqPVC)
	if err != nil {
		return
	}

	if reqPVC.Name == "" || reqPVC.ProjectID == 0 {
		n.customAbort(http.StatusBadRequest, "PVC Name and project ID should not null")
		return
	}

	//	pvcExists, err := service.PVCExists(reqPVC.ProjectID, reqPVC.Name)
	//	if err != nil {
	//		n.internalError(err)
	//		return
	//	}
	//	if pvcExists {
	//		n.customAbort(http.StatusConflict, "Node Group name already exists.")
	//		return
	//	}

	pvcID, err := service.CreatePVCDB(reqPVC)
	if err != nil {
		logs.Debug("Failed to add pvc %v", reqPVC)
		n.internalError(err)
		return
	}
	logs.Info("Added PVC %s %d", reqPVC.Name, pvcID)
}