package main

import (
    "log"
    "fmt"
    "time"
    mqtt "github.com/eclipse/paho.mqtt.golang"
    "github.com/ghodss/yaml"
    "io/ioutil"
    "github.com/agnivade/levenshtein" 
    "strings"
    "errors"
    "golang.org/x/text/runes"
    "golang.org/x/text/transform"
    "golang.org/x/text/unicode/norm"
    "unicode"
    "sort"
)

type Mqtt struct {
    Broker string
    User string
    Password string
    LedDeviceSrcTopic string
    LedDeviceDstTopic string
    TeleSrcTopic string  
    TeleDstTopic string  
    SensorDeviceSrcTopic string 
    SensorDeviceDstTopic string
}


type DeviceCmdCode struct {
    ChatCmd string
    DeviceCmd string
    ChatResponseMap map[string]string
}

type Command struct {
    DeviceCmdCodeArrVN []DeviceCmdCode
    DeviceCmdCodeArrEN []DeviceCmdCode
    DefaultRespMsg map[string]string 
    TickTimeout time.Duration
    StringRateThreshold float32
}

type FileConfig struct {
    MqttConfig Mqtt
    CmdConfig Command
}

type StringSearchResult int
const (
    AlmostSame StringSearchResult = iota
    Same    
    Different
)

type DeviceName int
const (
    LED_DEVICE DeviceName = iota
    SENSOR_DEVICE
)

type StringCompare struct {
    Data string
    RatePercent float32
}

var cfg FileConfig
var chatCmdlist[] string
var ledDeviceMqttClient mqtt.Client
var ledDeviceChannel chan string 
var cmdListMapVN map[string]*DeviceCmdCode
var cmdListMapEN map[string]*DeviceCmdCode
var sensorDeviceChannel chan string 
var sensorMqttClient mqtt.Client
var telegramMqttClient mqtt.Client

var messageTelePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
    teleMsg := string(msg.Payload())
    fmt.Printf("Received message: [%s] from topic: %s\n", teleMsg, msg.Topic())
    groupID, _ := getGroupIdTelegram(msg.Topic())
    handleTeleCmd(groupID, teleMsg)
}

var messageLedDevicePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
    ledDeviceMsg := string(msg.Payload())
    fmt.Printf("Received message: [%s] from topic: %s\n", ledDeviceMsg, msg.Topic())
    writeDeviceChannel(ledDeviceChannel, ledDeviceMsg)
}

var messageSensorDevicePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
    sensorDeviceMsg := string(msg.Payload())
    fmt.Printf("Received message: [%s] from topic: %s\n", sensorDeviceMsg, msg.Topic())
    writeDeviceChannel(sensorDeviceChannel, sensorDeviceMsg)
}


func checkDeviceName(script *DeviceCmdCode) DeviceName {
    var deviceName DeviceName

    _, checkKey := script.ChatResponseMap["Data"]
    if checkKey == true {
        deviceName = SENSOR_DEVICE
    }else {
        deviceName = LED_DEVICE
    } 

    return deviceName
}

func getGroupIdTelegram (topic string) (string, error) {
    topicItem := strings.Split(topic, "/")
    err := "Incorrect topic format"

    if topicItem[0] != "Telegram" {
        return "0", errors.New(err)
    }else {
        if topicItem[2] != "Rx" {
            return "0", errors.New(err)
        }else {
            groupID := topicItem[1]
            return groupID, nil
        }
    }
}

func getNormStr(inputStr string) string {
        lowerStr := strings.ToLower(inputStr)

        t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
        normStr, _, _ := transform.String(t, lowerStr)

        return normStr      
}

func getCommandSearchStatus(rateOfChange float32) StringSearchResult {
    cmdSearchRes := Different
    strRateThres := cfg.CmdConfig.StringRateThreshold

    if rateOfChange == 100.0 {
        cmdSearchRes = Same
    }else if rateOfChange >= strRateThres {
        cmdSearchRes = AlmostSame
    }

    return cmdSearchRes
}

func deviceCmdListInit(deviceCmdCodeArr []DeviceCmdCode,
                    msgTimeout string) map[string]*DeviceCmdCode {
    cmdListMap := make(map[string]*DeviceCmdCode)

    for i := 0 ; i < len(deviceCmdCodeArr); i++ {
        cmdListMap[deviceCmdCodeArr[i].ChatCmd] = &deviceCmdCodeArr[i]
        chatCmdlist = append(chatCmdlist, deviceCmdCodeArr[i].ChatCmd)
    }
    for _, controlLed :=range deviceCmdCodeArr {
        controlLed.ChatResponseMap["Timeout"] =  cfg.CmdConfig.DefaultRespMsg[msgTimeout] 
    } 

    return cmdListMap      
}

func handleDeviceScript(script *DeviceCmdCode, groupID string) {
    responseChannel := readResponseFromDeviceChannel(script)

    fmt.Printf("[Response channel: %s]", responseChannel)
    resDataTele, checkKeyExists := script.ChatResponseMap[responseChannel];
    switch checkKeyExists {
    case true:
        sendToTelegram(groupID, resDataTele)

    default:
        sendToTelegram(groupID, cfg.CmdConfig.DefaultRespMsg["ErrorCmd"])
    }
} 

func handleDeviceCmd(groupID string, cmd string) {
    scriptVN, checkKeyExistsVN := cmdListMapVN[cmd];
    scriptEN, _ := cmdListMapEN[cmd];
    if checkKeyExistsVN == true {
        handleDeviceScript(scriptVN, groupID)
    }else {
        handleDeviceScript(scriptEN, groupID)          
    }     
}

func handleTeleCmd(groupID string, cmd string) {  
    chatCmd := removeElementAfterBracket(cmd)  
    chatCmdArr := sortCommandCompareArrayDescending(chatCmd, chatCmdlist)
    maxRatePercent :=  chatCmdArr[0].RatePercent
    cmdSearchRes := getCommandSearchStatus(maxRatePercent)

    switch cmdSearchRes {
    case Different:
        sendHelpResponseToTelegramUser(groupID)
    case AlmostSame:
        sendSuggestResponseToTelegramUser(groupID, chatCmdArr)
    case Same:
        handleDeviceCmd(groupID, chatCmdArr[0].Data)         
    }
}

func mqttBegin(broker string, user string, pw string, messagePubHandler *mqtt.MessageHandler) mqtt.Client {
    var opts *mqtt.ClientOptions = new(mqtt.ClientOptions)

    opts = mqtt.NewClientOptions()
    opts.AddBroker(broker)
    opts.SetUsername(user)
    opts.SetPassword(pw) 
    opts.SetDefaultPublishHandler(*messagePubHandler)
    client := mqtt.NewClient(opts)
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        panic(token.Error())
    }

    return client
}

func sendHelpResponseToTelegramUser(groupID string) {
    var cmdKeyboard string
    textVN := cfg.CmdConfig.DefaultRespMsg["ResponseHelpVN"]
    textEN := cfg.CmdConfig.DefaultRespMsg["ResponseHelpEN"]
    textKeyboard := textVN + "\n" + textEN
    
    for _, cmd := range chatCmdlist {
        cmdKeyboard += "/" + cmd
    }

    sendToTelegram(groupID, "[" + textKeyboard + cmdKeyboard + "]")   
}

func sendSuggestResponseToTelegramUser (groupID string, chatCmdArr []StringCompare) {
    var textKeyboard string
    var cmdKeyboard string 
    chatCmd := chatCmdArr[0].Data

    _, checkKeyExistsVN := cmdListMapVN[chatCmd]
    if checkKeyExistsVN == true {
        textKeyboard = cfg.CmdConfig.DefaultRespMsg["SuggestVN"]
    }else {
        textKeyboard = cfg.CmdConfig.DefaultRespMsg["SuggestEN"]        
    }
    for i := 0; i < 3; i++ {
        rateValue := fmt.Sprintf("%.2f", chatCmdArr[i].RatePercent)
        cmdKeyboard += "/" + chatCmdArr[i].Data + " (" + rateValue + " %" + ")" 
    }

    sendToTelegram(groupID, "[" + textKeyboard + cmdKeyboard + "]")    
}

func sendToLedDevice(msg string) {
    ledDeviceMqttClient.Publish(cfg.MqttConfig.LedDeviceDstTopic, 0, false, msg)
}

func sendToSensorDevice(msg string) {
    sensorMqttClient.Publish(cfg.MqttConfig.SensorDeviceDstTopic, 0, false, msg)
}

func sendToTelegram(groupID string, msg string) {
    teleDstTopic := strings.Replace(cfg.MqttConfig.TeleDstTopic, "GroupID", groupID, 1)
    telegramMqttClient.Publish(teleDstTopic, 0, false, msg)
}

func sortChatCmdlist () []string{
    var cmdList []string
    halfLength := len(chatCmdlist) / 2

    for i := 0; i < halfLength; i++ {
        cmdList = append(cmdList, chatCmdlist[i])        
        cmdList = append(cmdList, chatCmdlist[i + halfLength])        
    }

    return cmdList
}

func sortCommandCompareArrayDescending(str string, strArr[] string) ([]StringCompare) {
    var strCmp StringCompare
    var chatCmdArr []StringCompare

    for i := 0; i < len(strArr); i++ {
        str1 := getNormStr(str)
        str2 := getNormStr(strArr[i])
        numTranStep := levenshtein.ComputeDistance(str1, str2)
        lenStr2 := len(str2)
        strCmp.Data = strArr[i]
        if numTranStep > lenStr2 {
            strCmp.RatePercent = 0.0
        }else {
            strCmp.RatePercent = 100.0 - (float32(numTranStep) / float32(len(str2)) * 100.0)
        }
        chatCmdArr = append(chatCmdArr, strCmp)
        // fmt.Printf("[%s - %d - %.2f]\n", getNormStr(strArr[i]), numTranStep, strCmp.RatePercent)
    }
    sort.SliceStable(chatCmdArr, func(i, j int) bool {
        return chatCmdArr[i].RatePercent > chatCmdArr[j].RatePercent
    })

    return chatCmdArr
}


func readDeviceChannel(deviceChannel chan string, timeOut time.Duration) string {
    var msg string

    select {
    case msg =  <-deviceChannel:
        return msg;
    case <-time.After(timeOut * time.Second):
        msg = "Timeout"
        return msg
    }
}

func readResponseFromDeviceChannel(script *DeviceCmdCode) string {
    var responseChannel string

    deviceName := checkDeviceName(script) 
    switch deviceName {
    case LED_DEVICE:
        sendToLedDevice(script.DeviceCmd)
        responseChannel = readDeviceChannel(ledDeviceChannel, cfg.CmdConfig.TickTimeout)            
    case SENSOR_DEVICE:
        sendToSensorDevice(script.DeviceCmd)
        responseChannel = readDeviceChannel(sensorDeviceChannel, cfg.CmdConfig.TickTimeout)
        if responseChannel != "Timeout" {
            dataMsg := strings.Split(script.ChatResponseMap["Data"], ":") 
            script.ChatResponseMap["Data"] = dataMsg[0] + ": " + responseChannel
            responseChannel = "Data"
        }
    }
    return responseChannel
}

func removeElementAfterBracket(strInput string) string {
    var strOutput string
    index := strings.Index(strInput, "(")
    if index == -1 {
        strOutput = strInput
    }else {
        strOutput = strInput[0:(index-1)]        
    }
    return strOutput
}

func writeDeviceChannel(deviceChannel chan string, cmd string) {
    deviceChannel <-cmd
}

func yamlFileHandle() {
    yfile, err := ioutil.ReadFile("config.yaml")

    if err != nil {

      log.Fatal(err)
    }

    err2 := yaml.Unmarshal(yfile, &cfg)

    if err2 != nil {
        fmt.Println("Error file yaml")

      log.Fatal(err2)
    }
}

func main() {
    yamlFileHandle()

    ledDeviceChannel = make(chan string, 1)
    sensorDeviceChannel = make(chan string, 1)

    cmdListMapVN = deviceCmdListInit(cfg.CmdConfig.DeviceCmdCodeArrVN, "TimeoutVN")
    cmdListMapEN = deviceCmdListInit(cfg.CmdConfig.DeviceCmdCodeArrEN, "TimeoutEN")
    chatCmdlist = sortChatCmdlist()

    ledDeviceMqttClient = mqttBegin(cfg.MqttConfig.Broker, cfg.MqttConfig.User, cfg.MqttConfig.Password, &messageLedDevicePubHandler)
    ledDeviceMqttClient.Subscribe(cfg.MqttConfig.LedDeviceSrcTopic, 1, nil)
    
    telegramMqttClient = mqttBegin(cfg.MqttConfig.Broker, cfg.MqttConfig.User, cfg.MqttConfig.Password, &messageTelePubHandler)
    telegramMqttClient.Subscribe(cfg.MqttConfig.TeleSrcTopic, 1, nil)

    sensorMqttClient = mqttBegin(cfg.MqttConfig.Broker, cfg.MqttConfig.User, cfg.MqttConfig.Password, &messageSensorDevicePubHandler)
    sensorMqttClient.Subscribe(cfg.MqttConfig.SensorDeviceSrcTopic, 1, nil)
    
    fmt.Println("Connected")
    for {
        time.Sleep(2 * time.Second)
    }    
}