package main

import (
    "log"
	"fmt"
    mqtt "github.com/eclipse/paho.mqtt.golang"
    "github.com/jacobsa/go-serial/serial"    
    "bufio"    
    "io"   
    "github.com/ghodss/yaml"
    "io/ioutil"
)

var portDevice io.ReadWriteCloser

var messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
    fmt.Printf("Received message: [%s] from topic: %s\n", msg.Payload(), msg.Topic())
    str := msg.Payload()
    newData := string(str) + "\n"

    data := []byte(newData)

    portDevice.Write(data)
    fmt.Printf("Send data: [%s] successfully to serial port\n", str)
}

type Mqtt struct {
    Broker string
    SerialDstTopic string
    SeialSrcTopic string
}

type Serial struct {
    ComName string
    Baudrate uint
}

type FileConfig struct {
    SerialConfig Serial    
    MqttConfig Mqtt
}
var cfg FileConfig

func yaml_file_handle() {
    yfile, err := ioutil.ReadFile("config.yaml")

    if err != nil {

      log.Fatal(err)
    }

    err2 := yaml.Unmarshal(yfile, &cfg)

    if err2 != nil {

      log.Fatal(err2)
    }  
}

func serialBegin(port string, baud uint) (io.ReadWriteCloser){
    var options serial.OpenOptions

    // Set up options.
    options = serial.OpenOptions{
      PortName: port,
      BaudRate: baud,
      DataBits: 8,
      StopBits: 1,
      MinimumReadSize: 4,
    }

    //Open the port.
    var portDev io.ReadWriteCloser
    var err error
    portDev, err = serial.Open(options)
    if err != nil {
      log.Fatalf("serial.Open: %v", err)
    }

    return portDev
}

func mqttBegin(broker string) mqtt.Client {

    var opts *mqtt.ClientOptions = new(mqtt.ClientOptions)

    opts = mqtt.NewClientOptions()
    opts.AddBroker(broker)

    opts.SetDefaultPublishHandler(messagePubHandler)

    client := mqtt.NewClient(opts)
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        panic(token.Error())
    }
    return client
}

func main() {
    //setup
    yaml_file_handle()

    portDevice = serialBegin(cfg.SerialConfig.ComName, cfg.SerialConfig.Baudrate)

    client := mqttBegin(cfg.MqttConfig.Broker)
    fmt.Println("Connected")

    client.Subscribe(cfg.MqttConfig.SerialDstTopic, 1, nil)
    

    //loop
    scanner := bufio.NewScanner(portDevice)
    for scanner.Scan() {
            client.Publish(cfg.MqttConfig.SeialSrcTopic, 0, false, scanner.Text())
            fmt.Printf("Publish: " + cfg.MqttConfig.SeialSrcTopic + ":" + scanner.Text() + "\n\n")
    }
}