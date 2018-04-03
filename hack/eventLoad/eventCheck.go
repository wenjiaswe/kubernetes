package main

import (
	"github.com/spf13/cobra"
	"fmt"
	"os"
	"bufio"
	"strings"
	//"sort"
	"log"
	//"flag"
)

const (
	message = `List 0 are the resourceVersions of events got from etcd (ETCD)
List 1 are the resourceVersions of events after processing event from etcd (OUTETCD)
List 2 are the resourceVersions of events going into API server (INAPISERVER)
List 3 are the resourceVersions of events sent out from API server (OUTAPISERVER)
List 4 are the resourceVersions of events got into scheduler (SCHEDULER)
List 5 are the resourceVersions of events got into controller-manager (CONTROLLERMANAGER)
Please find details here: https://github.com/kubernetes/kubernetes/pull/61067
`
)


var (
	eventCheckCmd = &cobra.Command{
		Short: "A tool to check if there is any missing event based on resourceVersion.",
		Long: message,
		Run: func(cmd *cobra.Command, args []string) {
			runEventCheck()
		},
	}
	ecOpts = eventCheckOpts{}
)

type eventCheckOpts struct {
	logDir string
	baseList string
}

var rvLists [][]string

func main() {

	flags := eventCheckCmd.Flags()
	flags.StringVar(&ecOpts.logDir, "logDir", "", "absolute path to the log directory")
	flags.StringVar(&ecOpts.baseList, "baseList", "ETCD", "base list that you want to compare with (choose one from etcd, outetcd, inapiserver, outapiserver, scheduler, controllermanager)")
	eventCheckCmd.Execute()

}

func runEventCheck(){
	var baseNum int
	switch strings.ToUpper(ecOpts.baseList){
	case "ETCD":
		baseNum = 0
	case "OUTETCD":
		baseNum = 1
	case "INAPISERVER":
		baseNum = 2
	case "OUTAPISERVER":
		baseNum = 3
	case "SCHEDULER":
		baseNum = 4
	case "CONTROLLERMANAGER":
		baseNum = 5
	default:
		baseNum = 0
	}

	apiLogEntries, err := readLogs(ecOpts.logDir + "/kube-apiserver.log")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	schedulerLogEntries, err := readLogs(ecOpts.logDir + "/kube-scheduler.log")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	controllerManagerLogEntries, err := readLogs(ecOpts.logDir + "/kube-controller-manager.log")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	podCnt, podsList := getPodsList(apiLogEntries)
	falsePodsCnt := 0

	fmt.Printf("\nChecking resourceVersion of event for each pod...\n")

	for _, podName := range podsList {
		rvLists = make([][]string, 6)
		fillRVList4APIServer(apiLogEntries, rvLists, podName)
		fillRVList4Client(schedulerLogEntries, rvLists, 4, podName)
		fillRVList4Client(controllerManagerLogEntries, rvLists, 5, podName)
		falseList, isSame := compareLists(baseNum, podName)
		if !isSame {
			fmt.Printf("Pod %s is not right in lists: ", podName)
			fmt.Println(falseList)
			falsePodsCnt ++
		}
	}

	//for _, rvList := range rvLists{
	//	sort.Strings(rvList)
	//	fmt.Println(rvList)
	//}
	fmt.Printf("\n%d out of %d pods have problem!\n", falsePodsCnt, podCnt)
}

func readLogs(logName string) ([]string, error) {
	fmt.Printf("Reading %s...\n", logName)
	logEntries, err := readLines(logName)
	if err != nil {
		log.Fatalf("Failed reading lines: %v", err)
	}
	return logEntries, err
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func getPodsList(apiLogEntries []string) (int, []string) {
	fmt.Printf("\nGeting pods list...")
	var pods []string
	podCnt := 0
	for _, line := range apiLogEntries {
		result := strings.Split(line, ",")
		eyeCatcher := result[0]
		if ( eyeCatcher == "eventTracker" ){
			loc := result[1]
			objName := result[5]
			reflectType := result[6]
			if (loc == "watch_cache/processEvent") && ( reflectType == "*core.Pod"){
				pods = AppendIfMissing(pods, objName)
				podCnt ++
			}
		}
	}
	fmt.Printf("\n%d pods found\n\n", podCnt)
	return podCnt, pods
}

func fillRVList4APIServer(lines []string, rvLists [][]string, eventKey string) {
	for _, line := range lines {
		result := strings.Split(line, ",")
		eyeCatcher := result[0]
		if ( eyeCatcher == "eventTracker" ) && ( eventKey == "" || result[5] == eventKey){
			rvStr := result[7]
			switch result[1] {
			case "etcd3/watcher/transform/curObj", "etcd3/watcher/transform/oldObj":
				rvLists[0] = AppendIfMissing(rvLists[0], rvStr)
			case "etcd3/watcher/processEvent":
				rvLists[1] = append(rvLists[1], rvStr)
			case "watch_cache/processEvent":
				rvLists[2] = append(rvLists[2], rvStr)
			case "cacher/dispatchEvent":
				rvLists[3] = append(rvLists[3], rvStr)
			//case "reflector/watchHandler":
			//	if sort.SearchStrings(rvLists[4], rvStr) == len(rvLists[4]) {
			//		rvLists[4] = append(rvLists[4], rvStr)
			//	}
			}
		}
	}
	return
}

func fillRVList4Client(lines []string, rvLists [][]string, listNum int, eventKey string) {
	for _, line := range lines {
		result := strings.Split(line, ",")
		eyeCatcher := result[0]
		if ( eyeCatcher == "eventTracker" ) && ( eventKey == "" || result[5] == eventKey){
			rvStr := result[7]
			if strings.HasPrefix(result[1], "reflector/watchHandler") {
				rvLists[listNum] = append(rvLists[listNum], rvStr)
			}else{
				fmt.Println(line)
			}
		}
	}
	return
}

func compareLists(baseNum int, podName string) ([]int, bool) {
	isSame := true
	baseList := rvLists[baseNum]
	var falseList []int
	for i := 0; i < 6; i++ {
		if i == baseNum {
			continue
		}
		if !compareWithBase(baseList, rvLists[i]) {
			if len(falseList) == 0 {
				falseList = append(falseList, baseNum)
			}
			falseList = append(falseList, i)
			fmt.Printf("OUCH!!! List %d is DIFFERENT from base list for pod %s\n", i, podName)
			isSame = false
		}
	}
	//fmt.Printf("%s, ", podName)
	return falseList, isSame
}

func compareWithBase(baseRvList []string, currRvList []string) bool{
	bl := len(baseRvList)
	cl := len(currRvList)
	if bl != cl {
		fmt.Printf("\nbase rv list has %d events but current rv list has %d events\n", bl, cl)
		return false
	}

	for i := 0; i < len(baseRvList); i++ {
		if baseRvList[i] != currRvList[i]{
			fmt.Printf("Check event with resourceVersion %d in base rv list and event with resourceVersion %d in current rv list\n", bl, cl)
			return false
		}
	}
	return true
}

func AppendIfMissing(slice []string, i string) []string {
	for _, ele := range slice {
		if ele == i {
			return slice
		}
	}
	return append(slice, i)
}