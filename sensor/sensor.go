package main

import (
    "fmt"
    "time"
    "strconv"
    mqtt "github.com/eclipse/paho.mqtt.golang"
)

var nodeMqttClient mqtt.Client
var valueSensor int

var messageNodeDevicePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
    sensorDeviceMsg := string(msg.Payload())
    fmt.Printf("Received message: [%s] from topic: %s\n", sensorDeviceMsg, msg.Topic())
    valueSensor += 1
    if sensorDeviceMsg == "HUMP" {
        sendToBot(strconv.Itoa(valueSensor))
    }
    if sensorDeviceMsg == "TEMP" {
        sendToBot("26")
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

func sendToBot(strMsg string ) {
    nodeMqttClient.Publish("TestSensor/Rx", 0, false, strMsg)
    fmt.Println("Publish: TestSensor/Rx" + ": " + strMsg)  
}

func main() {
    valueSensor = 40
    nodeMqttClient = mqttBegin("localhost:1883", "nmtam", "221220", &messageNodeDevicePubHandler)
    nodeMqttClient.Subscribe("TestSensor/Tx", 1, nil)
    
    fmt.Println("Connected")
    for {
        time.Sleep(2 * time.Second)
    }
}