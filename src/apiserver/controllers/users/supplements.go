package users

import (
	"git/inspursoft/board/src/apiserver/service"
	c "git/inspursoft/board/src/common/controller"
	"net/http"
)

// Operation about Users' supplement actions.
type SupplementController struct {
	c.BaseController
}

func (u *SupplementController) Prepare() {}

// @Title Supplement checking user existing.
// @Description Supplement for user existing.
// @Param	target	query	string 	true	"Request for probe key."
// @Param	value query	string	true	"Request for probe val."
// @Param	user_id	query	int	true	"Request for user ID."
// @Success 200 Successful checked.
// @Failure 400 Bad request.
// @Failure 401 Unauthorized.
// @Failure 403 Forbidden.
// @router /existing [get]
func (u *SupplementController) Exists() {
	target := u.GetString("target")
	value := u.GetString("value")
	userID, _ := u.GetInt64("user_id")
	isExists, err := service.UserExists(target, value, userID)
	if err != nil {
		u.InternalError(err)
		return
	}
	if isExists {
		u.CustomAbortAudit(http.StatusConflict, target+" already exists.")
	}
}