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
    SerialSrcTopic string
    SerialDstTopic string

    TeleSrcTopic string  
    TeleDstTopic string   
}


type LedControlCode struct {
    ChatCmd []string
    DeviceCmd string
    ChatResponseMap map[string]string
}

type Command struct {
    ControlLedVN []LedControlCode
    ControlLedEN []LedControlCode

    DefaultRespMsg map[string]string 
    TickTimeout time.Duration
}

type FileConfig struct {
    MqttConfig Mqtt
    CmdConfig Command
}

type StringSearchResults int
const (
    AlmostSame StringSearchResults = iota
    Same    
    Different
)

var cfg FileConfig
var mqttClientHandleTele mqtt.Client
var mqttClientHandleSerial mqtt.Client

var serialRXChannel chan string 
var cmdListMapVN map[string]*LedControlCode
var cmdListMapEN map[string]*LedControlCode
var listChatCmds[] string 

var messageTelePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {

    teleMsg := string(msg.Payload())

    fmt.Printf("Received message: [%s] from topic: %s\n", teleMsg, msg.Topic())
    groupID, _ := getGroupIdTelegram(msg.Topic())
    handleTeleCmd(groupID, teleMsg)
}

var messageSerialPubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
    serialMsg := string(msg.Payload())
    fmt.Printf("Received message: [%s] from topic: %s\n", serialMsg, msg.Topic())
    handleSerialCmd(serialMsg)
}

func cmdListMapInit(controlLedArr []LedControlCode,
                    resMsgTimeout string,
                    resMsgUnknowCmd string) map[string]*LedControlCode {
    var cmdStr string
    cmdListMap := make(map[string]*LedControlCode)

    for i := 0 ; i < len(controlLedArr); i++ {
        for j := 0; j < len(controlLedArr[i].ChatCmd); j++ {
            cmdListMap[controlLedArr[i].ChatCmd[j]] = &controlLedArr[i]
            listChatCmds = append(listChatCmds, controlLedArr[i].ChatCmd[j])
        }
        cmdStr += "/" + controlLedArr[i].ChatCmd[0]
    }
    cfg.CmdConfig.DefaultRespMsg[resMsgUnknowCmd] += cmdStr
    for _, controlLed :=range controlLedArr {
        controlLed.ChatResponseMap["Timeout"] =  cfg.CmdConfig.DefaultRespMsg[resMsgTimeout] 
    } 

    return cmdListMap      
}

func findTheMostSimilarString(str string, strArr[] string) (string, StringSearchResults) {
    minNumStep := 7
    resStr := "NULL"
    numTransStep := - 1
    cmpSta := Different

    for i := 0; i < len(strArr); i++ {
        numTransStep = levenshtein.ComputeDistance(getNormStr(str), getNormStr(strArr[i]))
        // fmt.Printf("[%s - %d]\n", getNormStr(strArr[i]), numTransStep)
        if numTransStep < minNumStep {
            minNumStep = numTransStep
            resStr = strArr[i]
        }
    }
    if minNumStep == 0 {
        cmpSta = Same
    }else if minNumStep < 7 {
        cmpSta = AlmostSame
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

func handleTeleScript(script *LedControlCode, groupID string, chatCmd string) {

    sendToSerial(script.DeviceCmd)

    resRxChan := readSerialRXChannel(cfg.CmdConfig.TickTimeout)

    resDataTele, checkKeyExists := script.ChatResponseMap[resRxChan];

    switch checkKeyExists {
        case true:
            sendToTelegram(groupID, resDataTele)

        default:
            sendToTelegram(groupID, cfg.CmdConfig.DefaultRespMsg["ErrorCmd"])
    }
} 

func handleTeleCmd(groupID string, chatCmd string) {  
    resStr, strSearchResults := findTheMostSimilarString(chatCmd, listChatCmds)
    // fmt.Printf("[cmd: %s - num: %d]\n", resStr, numTransStep)

    switch strSearchResults {
        case Different:
            helpResVN := "[" + cfg.CmdConfig.DefaultRespMsg["ResponseHelpVN"] + "]"
            helpResEN := "[" + cfg.CmdConfig.DefaultRespMsg["ResponseHelpEN"] + "]"
            sendToTelegram(groupID, helpResVN)
            sendToTelegram(groupID, helpResEN)  
        case AlmostSame:
            var msgResponse string
            _, checkKeyVN := cmdListMapVN[resStr];
            if checkKeyVN == true {
                msgResponse = "[" + cfg.CmdConfig.DefaultRespMsg["HintQuestionVN"] + "/" + resStr + "]"                  
            }else {
                msgResponse = "[" + cfg.CmdConfig.DefaultRespMsg["HintQuestionEN"] + "/" + resStr + "]"
            }
            sendToTelegram(groupID, msgResponse)
        case Same:
            // fmt.Println("[Dieu khien thanh cong]")
            scriptVN, checkKeyExistsVN := cmdListMapVN[resStr];
            scriptEN, _ := cmdListMapEN[chatCmd];
            if checkKeyExistsVN == true {
                handleTeleScript(scriptVN, groupID, resStr)
            }else {
                handleTeleScript(scriptEN, groupID, resStr)          
            }            
    }
}

func handleSerialCmd(cmd string) {
    serialRXChannel <-cmd
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

func readSerialRXChannel(timeOut time.Duration) string {
    var msg string

    select {
        case msg =  <-serialRXChannel:
            return msg;
        case <-time.After(timeOut * time.Second):
            msg = "Timeout"
            return msg
    }
}

func sendToSerial(msg string) {
    mqttClientHandleTele.Publish(cfg.MqttConfig.SerialDstTopic, 0, false, msg)
}

func sendToTelegram(groupID string, msg string) {
    teleDstTopic := strings.Replace(cfg.MqttConfig.TeleDstTopic, "GroupID", groupID, 1)
    mqttClientHandleSerial.Publish(teleDstTopic, 0, false, msg)
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
    serialRXChannel = make(chan string, 1)
    yamlFileHandle()
    cmdListMapVN = cmdListMapInit(cfg.CmdConfig.ControlLedVN, "TimeoutVN", "ResponseHelpVN")
    cmdListMapEN = cmdListMapInit(cfg.CmdConfig.ControlLedEN, "TimeoutEN", "ResponseHelpEN")
    mqttClientHandleTele = mqttBegin(cfg.MqttConfig.Broker, cfg.MqttConfig.User, cfg.MqttConfig.Password, &messageTelePubHandler)
    mqttClientHandleTele.Subscribe(cfg.MqttConfig.TeleSrcTopic, 1, nil)
    mqttClientHandleSerial = mqttBegin(cfg.MqttConfig.Broker, cfg.MqttConfig.User, cfg.MqttConfig.Password, &messageSerialPubHandler)
    mqttClientHandleSerial.Subscribe(cfg.MqttConfig.SerialSrcTopic, 1, nil)
    fmt.Println("Connected")

    for {

        time.Sleep(2 * time.Second)
    }
}