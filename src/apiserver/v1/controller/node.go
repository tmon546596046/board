package controller

import (
	"fmt"
	"git/inspursoft/board/src/apiserver/service"
	c "git/inspursoft/board/src/common/controller"
	"git/inspursoft/board/src/common/utils"
	"net/http"

	"github.com/astaxie/beego/logs"
)

type NodeController struct {
	c.BaseController
}

func (n *NodeController) GetNode() {
	para := n.GetString("node_name")
	res, err := service.GetNode(para)
	if err != nil {
		n.CustomAbortAudit(http.StatusInternalServerError, fmt.Sprint(err))
		return
	}
	n.RenderJSON(res)
}

func (n *NodeController) NodeToggle() {
	if !n.IsSysAdmin {
		n.CustomAbortAudit(http.StatusForbidden, "user should be admin")
		return
	}

	var responseStatus bool
	var err error
	paraName := n.GetString("node_name")
	paraStatus, _ := n.GetBool("node_status")

	switch paraStatus {
	case true:
		responseStatus, err = service.ResumeNode(paraName)
	case false:
		responseStatus, err = service.SuspendNode(paraName)
	}
	if err != nil {
		n.CustomAbortAudit(http.StatusInternalServerError, fmt.Sprint(err))
		return
	}
	if responseStatus != true {
		n.CustomAbortAudit(http.StatusPreconditionFailed, fmt.Sprint(err))
	}
}

func (n *NodeController) NodeList() {
	ping, _ := n.GetBool("ping")
	nodeList := service.GetNodeList()
	if ping {
		availableNodeList := []service.NodeListResult{}
		for _, node := range nodeList {
			status, err := utils.PingIPAddr(node.NodeIP)
			if err != nil {
				logs.Error("Failed to ping IPAddr: %s, error: %+v", node.NodeIP, err)
			}
			if status {
				availableNodeList = append(availableNodeList, node)
				break
			}
		}
		n.RenderJSON(availableNodeList)
		return
	}
	n.RenderJSON(nodeList)
}

func (n *NodeController) AddNodeToGroupAction() {
	//TODO node_id is not reay, should implement it
	//nodeID, err := strconv.Atoi(p.Ctx.Input.Param(":id"))

	nodeName := n.GetString("node_name")
	groupName := n.GetString("groupname")
	logs.Debug("Adding %s to %s", nodeName, groupName)

	//TODO check existing
	err := service.AddNodeToGroup(nodeName, groupName)
	if err != nil {
		n.InternalError(err)
		return
	}
}

func (n *NodeController) GetGroupsOfNodeAction() {

	//TODO node_id is not reay, should implement it
	//nodeID, err := strconv.Atoi(p.Ctx.Input.Param(":id"))

	nodeName := n.GetString("node_name")

	// Get the nodegroups of this node
	groups, err := service.GetGroupOfNode(nodeName)
	if err != nil {
		logs.Error("Failed to get node %s group", nodeName)
		n.InternalError(err)
		return
	}
	n.RenderJSON(groups)
}

func (n *NodeController) RemoveNodeFromGroupAction() {
	//TODO node_id is not reay, should implement it
	//nodeID, err := strconv.Atoi(p.Ctx.Input.Param(":id"))

	nodeName := n.GetString("node_name")
	groupName := n.GetString("groupname")
	//logs.Debug("Remove %s from %s", nodeName, groupName)

	err := service.RemoveNodeFromGroup(nodeName, groupName)
	if err != nil {
		n.InternalError(err)
		return
	}
	logs.Debug("Removed %s from %s", nodeName, groupName)
}

func (n *NodeController) NodesAvailalbeResources() {
	logs.Debug("GetNodesResources")
	resources, err := service.GetNodesAvailableResources()
	if err != nil {
		n.InternalError(err)
		return
	}

	n.RenderJSON(resources)
}