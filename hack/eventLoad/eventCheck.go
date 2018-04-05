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
	podName string
	podNameHas string
}

type eventTrackerEntry struct {
	dataPoint       string
	timestamp       string
	eventType       string
	namespace       string
	objName         string
	reflectType     string
	resourceVersion string
}

var rvLists [][]string

func main() {

	flags := eventCheckCmd.Flags()
	flags.StringVar(&ecOpts.logDir, "logDir", "", "absolute path to the log directory")
	flags.StringVar(&ecOpts.baseList, "baseList", "ETCD", "base list that you want to compare with (choose one from etcd, outetcd, inapiserver, outapiserver, scheduler, controllermanager)")
	flags.StringVar(&ecOpts.podName, "podName", "", "interested pod name")
	flags.StringVar(&ecOpts.podNameHas, "podNameHas", "", "interested in pods whose name contains this string")
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

	apiEventList, apiLogEntries, err := readLogs(ecOpts.logDir + "/kube-apiserver.log")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	schedulerEventList, schedulerLogEntries, err := readLogs(ecOpts.logDir + "/kube-scheduler.log")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	controllerEventList, controllerManagerLogEntries, err := readLogs(ecOpts.logDir + "/kube-controller-manager.log")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	podsList := getPodsList(apiLogEntries)
	podCnt := len(podsList)
	fmt.Printf("\n%d pods found\n\n", podCnt)
	falsePodsCnt := 0

	fmt.Printf("\nChecking resourceVersion of event for each pod...\n")
	var falsePodList []string

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
			falsePodList = append(falsePodList, podName)
		}
	}

	//for _, rvList := range rvLists{
	//	sort.Strings(rvList)
	//	fmt.Println(rvList)
	//}
	if falsePodsCnt != 0 {
		fmt.Printf("Here are the false pods:\n")
		fmt.Println(falsePodList)
		fmt.Println()
		if ecOpts.podName != "" {
			fmt.Printf("\nList events for pod %s\n", ecOpts.podName)
			fmt.Println("\nEvent in apiserver: ")
			listEntry4FalsePod(ecOpts.podName, apiEventList)
			fmt.Println("\nEvent in scheduler: ")
			listEntry4FalsePod(ecOpts.podName, schedulerEventList)
			fmt.Println("\nEvent in controller-manager: ")
			listEntry4FalsePod(ecOpts.podName, controllerEventList)
		} else if ecOpts.podName == "all"{
			for _, podName := range falsePodList {
				fmt.Println("Event in apiserver: ")
				listEntry4FalsePod(podName, apiEventList)
				fmt.Println("Event in scheduler: ")
				listEntry4FalsePod(podName, schedulerEventList)
				fmt.Println("Event in controller-manager: ")
				listEntry4FalsePod(podName, controllerEventList)
			}
		} else if ecOpts.podNameHas != "" {
			newPodLists := getNewPodNameList(ecOpts.podNameHas, falsePodList)
			for _, podName := range newPodLists {
				fmt.Printf("\nList events for pod %s\n", ecOpts.podName)
				fmt.Println("\nEvent in apiserver: ")
				listEntry4FalsePod(podName, apiEventList)
				fmt.Println("\nEvent in scheduler: ")
				listEntry4FalsePod(podName, schedulerEventList)
				fmt.Println("\nEvent in controller-manager: ")
				listEntry4FalsePod(podName, controllerEventList)
			}
		}
	}
	fmt.Printf("\n%d out of %d pods have problem!\n", falsePodsCnt, podCnt)
}

func readLogs(logName string) ( []eventTrackerEntry, []string, error) {
	fmt.Printf("Reading %s...\n", logName)
	eventLists, logEntries, err := readLines(logName)
	if err != nil {
		log.Fatalf("Failed reading lines: %v", err)
	}
	return eventLists, logEntries, err
}

func readLines(path string) ( []eventTrackerEntry, []string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	var lines []string
	var eventEntryList []eventTrackerEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		currLine := scanner.Text()
		result := strings.Split(currLine, ",")
		if result[0] == "eventTracker"{
			lines = append(lines, currLine)

			var currEntry eventTrackerEntry
			currEntry.dataPoint = result[1]
			currEntry.timestamp = result[2]
			currEntry.eventType = result[3]
			currEntry.namespace = result[4]
			currEntry.objName   = result[5]
			currEntry.reflectType = result[6]
			currEntry.resourceVersion = result[7]
			eventEntryList = append(eventEntryList, currEntry)
		}
	}
	return eventEntryList, lines, scanner.Err()
}

func getPodsList(apiLogEntries []string) []string {
	fmt.Printf("\nGeting pods list...")
	var pods []string
	for _, line := range apiLogEntries {
		result := strings.Split(line, ",")
		eyeCatcher := result[0]
		if ( eyeCatcher == "eventTracker" ){
			loc := result[1]
			objName := result[5]
			reflectType := result[6]
			if (loc == "watch_cache/processEvent") && ( reflectType == "*core.Pod"){
				pods = AppendIfMissing(pods, objName)
			}
		}
	}
	return pods
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
			isSame = false
			fmt.Printf("OUCH!!! List %d is DIFFERENT from base list for pod %s\n", i, podName)
			fmt.Printf("baseList %d: \n", baseNum)
			fmt.Println(baseList)
			fmt.Printf("currList %d: \n", i)
			fmt.Println(rvLists[i])
		}
	}
	//fmt.Printf("%s, ", podName)
	return falseList,isSame
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

func listEntry4FalsePod (podName string, apiEventList []eventTrackerEntry){
	var entries []eventTrackerEntry
	for _, entry := range apiEventList {
		if entry.objName == podName {
			entries = append(entries, entry)
			fmt.Println(entry)
		}
	}
	return
}

func getNewPodNameList (podNameHas string, falsePodList []string) []string {
	var newPodList []string
	for _,falsePod := range falsePodList {
		if strings.Contains(falsePod, podNameHas){
			newPodList = append(newPodList, falsePod)
		}
	}
	return newPodList
}