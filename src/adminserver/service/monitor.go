package service

import (
	"git/inspursoft/board/src/adminserver/models"
	"os/exec"
	"regexp"
	"strings"
)

//GetMonitor returns Board containers' information.
func GetMonitor() ([]*models.Boardinfo, error) {

	command := "docker ps -a --format \"table {{.ID}}\\t{{.Image}}\\t{{.CreatedAt}}\\t{{.Status}}\\t{{.Ports}}\" | grep " + models.ImagePrefix
	cmd := exec.Command("sh", "-c", command)
	bytes, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	resp := string(bytes)
	row := strings.Count(resp, "\n")
	arr := strings.Split(resp, "\n")

	//var containers [row]*models.Boardinfo
	containersAdd := make([]*models.Boardinfo, row)
	containersVal := make([]models.Boardinfo, row)

	command2 := "docker stats -a --no-stream --format \"table {{.Name}}\\t{{.CPUPerc}}\\t{{.MemUsage}}\\t{{.NetIO}}\\t{{.BlockIO}}\\t{{.MemPerc}}\\t{{.PIDs}}\" | grep " + models.ContainerPrefix
	cmd2 := exec.Command("sh", "-c", command2)
	bytes2, err := cmd2.Output()
	if err != nil {
		return nil, err
	}
	resp2 := string(bytes2)
	arr2 := strings.Split(resp2, "\n")

	reg, _ := regexp.Compile("\\s{2,}")
	for i := 0; i < row; i++ {
		items := reg.Split(arr[i], -1)

		//assign port with null if missing.
		if len(items) < 5 {
			items = append(items, "")
		}

		itemsStats := reg.Split(arr2[i], -1)
		items = append(items, itemsStats...)

		containersVal[i] = models.GetBoardinfo(items)
		containersAdd[i] = &containersVal[i]
		//fmt.Printf("%q\n", items)
	}

	return containersAdd, nil
}
