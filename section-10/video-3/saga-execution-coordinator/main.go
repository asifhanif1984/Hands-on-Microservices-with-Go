package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PacktPublishing/Hands-on-Microservices-with-Go/section-10/video-3/saga-execution-coordinator/repositories"
	"github.com/PacktPublishing/Hands-on-Microservices-with-Go/section-10/video-3/saga-execution-coordinator/sagaQuee"
	"github.com/PacktPublishing/Hands-on-Microservices-with-Go/section-10/video-3/saga-execution-coordinator/sagaStateMachine"

	"github.com/Shopify/sarama"
	"github.com/wvanbergen/kafka/consumergroup"
)

func main() {
	consumerGroupName := "buyVideoSagaConsumer"
	topic := "buyVideoSaga"

	consConfig := consumergroup.NewConfig()
	consConfig.Offsets.Initial = sarama.OffsetNewest
	consConfig.Offsets.ProcessingTimeout = 10 * time.Second

	// Specify brokers address. This is default one
	consBrokers := []string{"localhost:2181"}

	// Create new consumer
	consumer, err := consumergroup.JoinConsumerGroup(consumerGroupName, []string{topic}, consBrokers, consConfig)
	if err != nil {
		log.Fatal(err)
	}
	defer consumer.Close()

	prodConfig := sarama.NewConfig()
	prodConfig.Producer.RequiredAcks = sarama.WaitForAll
	prodConfig.Producer.Retry.Max = 5
	prodConfig.Producer.Return.Successes = true

	prodBrokers := []string{"localhost:9092"}
	producer, err := sarama.NewAsyncProducer(prodBrokers, prodConfig)
	if err != nil {
		log.Fatal(err)
	}
	defer producer.Close()

	ssm := &sagaStateMachine.SagaStateMachine{
		VideosRepo: &repositories.RestVideosRepository{},
		UsersRepo:  &repositories.RestUsersRepository{},
		AgentsRepo: &repositories.RestAgentsRepository{},
	}

	// Register Signal for exiting program. Ctrl C on Linux.
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		log.Println("Exiting System.")
		os.Exit(1)
	}()

	sagaQuee := sagaQuee.NewSagaQuee(ssm, topic, consumer, producer)

	go func() {
		sagaQuee.StartQueeProcessing()
	}()

	log.Println("Starting Quee Processing.")
}

func createStartHandler(sq *sagaQuee.SagaQuee) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		bvsDTO := &repositories.BuyVideoSagaDTO{}
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}
		err = json.Unmarshal(body, bvsDTO)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}
		sq.StartSaga(bvsDTO)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Saga Started."))
	}
}

func createCheckSagaStatueHandler(sq *sagaQuee.SagaQuee) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		bvsDTO := &repositories.BuyVideoSagaDTO{}
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}
		err = json.Unmarshal(body, bvsDTO)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}
		v, ok := sq.SuccesfulEndedSagas.Load(sq.CreateKey(bvsDTO.UserID, bvsDTO.VideoID))
		if ok && v.(bool) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Saga Ended."))
		}
		v, ok = sq.UnsuccesfulEndedSagas.Load(sq.CreateKey(bvsDTO.UserID, bvsDTO.VideoID))
		if ok && v.(bool) {
			w.WriteHeader(http.StatusPreconditionFailed)
			w.Write([]byte("Saga Rollbacked."))
		}

		w.WriteHeader(http.StatusProcessing)
		w.Write([]byte("Saga being Processed."))
	}
}
