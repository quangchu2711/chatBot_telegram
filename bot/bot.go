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
}

type FileConfig struct {
    MqttConfig Mqtt
    CmdConfig Command
}

type StringSearchResults int
const (
    ALMOST_SAME StringSearchResults = iota
    SAME    
    DIFFERENT
)

type DeviceName int
const (
    LED_DEVICE DeviceName = iota
    SENSOR_DEVICE
)

var cfg FileConfig
var ledDeviceMqttClient mqtt.Client
var ledDeviceChannel chan string 
var deviceCmdListVN map[string]*DeviceCmdCode
var deviceCmdListEN map[string]*DeviceCmdCode
var listAllChatCmd[] string
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

func findTheMostSimilarString(str string, strArr[] string) (string, StringSearchResults) {
    minNumStep := 9
    resStr := "NULL"
    numTransStep := - 1
    cmpSta := DIFFERENT

    for i := 0; i < len(strArr); i++ {
        numTransStep = levenshtein.ComputeDistance(getNormStr(str), getNormStr(strArr[i]))
        // fmt.Printf("[%s - %d]\n", getNormStr(strArr[i]), numTransStep)
        if numTransStep < minNumStep {
            minNumStep = numTransStep
            resStr = strArr[i]
        }
    }
    if minNumStep == 0 {
        cmpSta = SAME
    }else if minNumStep < 7 {
        cmpSta = ALMOST_SAME
    }

    return resStr, cmpSta
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

func deviceCmdListInit(ledControlCodeArr []DeviceCmdCode,
                        resTimeoutMsg string,
                        resMsgUnknowCmd string) map[string]*DeviceCmdCode {
    var cmdStr string
    ledDeviceCmdListMap := make(map[string]*DeviceCmdCode)

    for i := 0 ; i < len(ledControlCodeArr); i++ {
        ledDeviceCmdListMap[ledControlCodeArr[i].ChatCmd] = &ledControlCodeArr[i]
        listAllChatCmd = append(listAllChatCmd, ledControlCodeArr[i].ChatCmd)
        cmdStr += "/" + ledControlCodeArr[i].ChatCmd
    }
    cfg.CmdConfig.DefaultRespMsg[resMsgUnknowCmd] += cmdStr
    for _, ledControl := range ledControlCodeArr {
        ledControl.ChatResponseMap["Timeout"] =  cfg.CmdConfig.DefaultRespMsg[resTimeoutMsg] 
    }

    return ledDeviceCmdListMap      
}

func handleDeviceScript(script *DeviceCmdCode, groupID string) {
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
    fmt.Printf("[Response channel: %s]", responseChannel)
    resDataTele, checkKeyExists := script.ChatResponseMap[responseChannel];
    switch checkKeyExists {
    case true:
        sendToTelegram(groupID, resDataTele)

    default:
        sendToTelegram(groupID, cfg.CmdConfig.DefaultRespMsg["ErrorCmd"])
    }
} 

func handleDeviceCmd(groupID string, ledDeviceCmd string) {
    scriptVN, checkKeyExistsVN := deviceCmdListVN[ledDeviceCmd];
    scriptEN, _ := deviceCmdListEN[ledDeviceCmd];
    if checkKeyExistsVN == true {
        handleDeviceScript(scriptVN, groupID)
    }else {
        handleDeviceScript(scriptEN, groupID)          
    }     
}

func handleTeleCmd(groupID string, chatCmd string) {  
    resStr, strSearchResults := findTheMostSimilarString(chatCmd, listAllChatCmd)

    switch strSearchResults {
    case DIFFERENT:
        sendHelpResponseToUserTelegram(groupID) 
    case ALMOST_SAME:
        sendSuggestionResponseToUserTelegram(groupID, resStr)
    case SAME:
        handleDeviceCmd(groupID, resStr)         
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

func sendToLedDevice(msg string) {
    telegramMqttClient.Publish(cfg.MqttConfig.LedDeviceDstTopic, 0, false, msg)
}

func sendToSensorDevice(msg string) {
    telegramMqttClient.Publish(cfg.MqttConfig.SensorDeviceDstTopic, 0, false, msg)
}

func sendToTelegram(groupID string, msg string) {
    teleDstTopic := strings.Replace(cfg.MqttConfig.TeleDstTopic, "GroupID", groupID, 1)
    ledDeviceMqttClient.Publish(teleDstTopic, 0, false, msg)
}

func sendHelpResponseToUserTelegram(groupID string) {
    helpResVN := "[" + cfg.CmdConfig.DefaultRespMsg["ResponseHelpVN"] + "]"
    helpResEN := "[" + cfg.CmdConfig.DefaultRespMsg["ResponseHelpEN"] + "]"
    sendToTelegram(groupID, helpResVN)
    sendToTelegram(groupID, helpResEN)     
}

func sendSuggestionResponseToUserTelegram(groupID string, resMsg string) {
    var msgResponse string
    _, checkKeyVN := deviceCmdListVN[resMsg];
    if checkKeyVN == true {
        msgResponse = "[" + cfg.CmdConfig.DefaultRespMsg["HintQuestionVN"] + "/" + resMsg + "]"                  
    }else {
        msgResponse = "[" + cfg.CmdConfig.DefaultRespMsg["HintQuestionEN"] + "/" + resMsg + "]"
    }
    sendToTelegram(groupID, msgResponse)
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

    deviceCmdListVN = deviceCmdListInit(cfg.CmdConfig.DeviceCmdCodeArrVN, "TimeoutVN", "ResponseHelpVN")
    deviceCmdListEN = deviceCmdListInit(cfg.CmdConfig.DeviceCmdCodeArrEN, "TimeoutEN", "ResponseHelpEN")

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