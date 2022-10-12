package main

import (
    "log"
    "fmt"
    mqtt "github.com/eclipse/paho.mqtt.golang"
    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "github.com/ghodss/yaml"
    "io/ioutil"
    "strings"
    "strconv"
    "errors"
)

type Mqtt struct {
    Broker string
    User string
    Password string
    TeleSrcTopic string
    TeleDstTopic string   
}

type Telegram struct {
    BotName string
    BotToken string
    IdBotChat int64
}

type FileConfig struct {
    TelegramConfig Telegram    
    MqttConfig Mqtt
}
var cfg FileConfig

var bot *tgbotapi.BotAPI
var updates tgbotapi.UpdatesChannel
var mqttClient mqtt.Client

var messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
    fmt.Printf("Received message: [%s] from topic: %s\n", msg.Payload(), msg.Topic())
    groupID, _ := getGroupIdOfTopic(msg.Topic())

    ConvertMsgString := string(msg.Payload())
    textMsg, cmdMsg := checkInlineButtonMsg(ConvertMsgString)
    switch textMsg {
        case "NULL":
            sendNormalMsgToTeleGroup(groupID, ConvertMsgString)        
        default:
            sendInlineButtonMsgToTelegramGroup(groupID, textMsg, cmdMsg)
    }
}

func checkInlineButtonMsg (mqttMsg string) (string, []string) {
    textMsg := "NULL"
    var cmdMsg []string

    lenMqttMsg := len(mqttMsg)
    if mqttMsg[0] == '[' && mqttMsg[lenMqttMsg - 1] == ']' {
        inlineMsg := mqttMsg[1:(lenMqttMsg-1)]
        splitInlineMsg := strings.Split(inlineMsg, "/")
        textMsg = splitInlineMsg[0]
        cmdMsg = splitInlineMsg[1:len(splitInlineMsg)]
    }

    return textMsg, cmdMsg    
}

func getGroupIdOfTopic (topic string) (int64, error) {
    topicItem := strings.Split(topic, "/")

    err := "Incorrect topic format"
    if topicItem[0] != "Telegram" {
        return 0, errors.New(err)
    }else {
        if topicItem[2] != "Tx" {
            return 0, errors.New(err)
        }else {
            groupID, _ := strconv.Atoi(topicItem[1])
            return int64(groupID), nil
        }
    }
}

func mqttBegin(broker string, user string, pw string) mqtt.Client {
    var opts *mqtt.ClientOptions = new(mqtt.ClientOptions)

    opts = mqtt.NewClientOptions()
    opts.AddBroker(fmt.Sprintf(broker))
    // opts.SetUsername(cfg.MqttConfig.User)
    opts.SetUsername(user)
    // opts.SetPassword(cfg.MqttConfig.Password)   
    opts.SetPassword(pw)   
    // opts.SetCleanSession(true)

    opts.SetDefaultPublishHandler(messagePubHandler)
    client := mqtt.NewClient(opts)
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        panic(token.Error())
    }

    return client
}

func sendNormalMsgToTeleGroup(groupID int64, msg string) {    
    message := tgbotapi.NewMessage(groupID, msg)
    
    bot.Send(message)
}

func sendInlineButtonMsgToTelegramGroup(groupID int64, textDisplay string, cmdArr []string) {
    messageSendTele := tgbotapi.NewMessage(groupID, textDisplay)
    var inlineCmd  tgbotapi.InlineKeyboardButton
    var rowInlineCmd []tgbotapi.InlineKeyboardButton
    var keyboard [][]tgbotapi.InlineKeyboardButton

    for _, text := range cmdArr {
        inlineCmd.Text = text
        cmd := text
        inlineCmd.CallbackData = &cmd
        rowInlineCmd = append(rowInlineCmd, inlineCmd)
    }
    for i := 0; i < len(rowInlineCmd); i++ {
        sliceRowInlineCmd := rowInlineCmd[i:(i+1)]
        keyboard = append(keyboard, sliceRowInlineCmd)
    }
    messageSendTele.ReplyMarkup = tgbotapi.InlineKeyboardMarkup{
        InlineKeyboard: keyboard,
    }

    bot.Send(messageSendTele)  
}


func sendToBot(updateGroupId int64, updateText string ) {
    groupID := strconv.FormatInt(updateGroupId, 10)

    teleSrcTopic := strings.Replace(cfg.MqttConfig.TeleSrcTopic, "GroupID", groupID, 1)
    mqttClient.Publish(teleSrcTopic, 0, false, updateText)
    fmt.Println(cfg.MqttConfig.TeleSrcTopic + ": " + updateText)  
}

func telegramBotBegin(bot_token string) (tgbotapi.UpdatesChannel, *tgbotapi.BotAPI) {      
    var telegramBot *tgbotapi.BotAPI
    var telgramUpdates tgbotapi.UpdatesChannel

    telegramBot, _ = tgbotapi.NewBotAPI(bot_token)
    log.Printf("Authorized on account %s", telegramBot.Self.UserName)
    u := tgbotapi.NewUpdate(0)
    telgramUpdates = telegramBot.GetUpdatesChan(u)

    return telgramUpdates, telegramBot     
}

func yamlFileHandle() {
    yfile, err1 := ioutil.ReadFile("config.yaml")

    if err1 != nil {

      log.Fatal(err1)
    }
    err2 := yaml.Unmarshal(yfile, &cfg)

    if err2 != nil {

      log.Fatal(err2)
    }
}

func main() {
    yamlFileHandle()
    mqttClient = mqttBegin(cfg.MqttConfig.Broker, cfg.MqttConfig.User, cfg.MqttConfig.Password)
    mqttClient.Subscribe(cfg.MqttConfig.TeleDstTopic, 1, nil)
    updates, bot = telegramBotBegin(cfg.TelegramConfig.BotToken)

    for update := range updates {
        if update.Message != nil {
            sendToBot(update.Message.Chat.ID, update.Message.Text)         
        }else if update.CallbackQuery != nil {
            callback := tgbotapi.NewCallback(update.CallbackQuery.ID, update.CallbackQuery.Data)
            if _, err := bot.Request(callback); err != nil {
                panic(err)
            }
            sendToBot(update.CallbackQuery.Message.Chat.ID, update.CallbackQuery.Data)
        }
    }
}