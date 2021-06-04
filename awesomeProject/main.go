package main

import (
	"context"
	"encoding/json"
	"github.com/adshao/go-binance/v2"
	"github.com/dariubs/percent"
	"github.com/levigross/grequests"
	"io"
	"log"
	"os"
	"strconv"
	"sync"
	"time"
)
const (
	tryBuyPrice = 99.97
	//tryBuyPrice = 100.2
	minDiff = 0.19
	diffBetween = 0.16
	signalServer = "93.104.208.248:80"
	symbol = "BTCUSDT"
)

var client *binance.Client
var usdAmount float64
var btcAmount float64
var signal *Signal
var signalChannel chan []byte
var quit chan bool
var wasLastSignalSuccessful bool
type Signal struct {
	Step string `json:"s"`
	TypeSignal string `json:"t"`
}

type Config struct {
	TryBuyPrice     float64
	MinDiff         float64
	DiffBetween     float64
	SignalServer    string
	Symbol          string
	StopLossPercent float64
}
var config Config
var lastSignalReceived int64
func main() {
	quit = make(chan bool, 10000)
	wasLastSignalSuccessful = false
	lastSignalReceived = time.Now().Unix()
	logFile, err := os.OpenFile("/home/root/log_real.txt", os.O_RDWR | os.O_CREATE | os.O_APPEND, 0777)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer logFile.Close()
	mw := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(mw)

	file, err := os.OpenFile("/home/root/conf.json", os.O_RDWR | os.O_CREATE | os.O_APPEND, 0777)
	if err != nil {
		log.Fatalln("Ferror:", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	config = Config{}
	err = decoder.Decode(&config)
	if err != nil {
		log.Fatalln("error:", err)
	}
	log.Println(config)
	log.Println("LOADED CONFIG")

	signalChannel = make(chan []byte, 1000)
	signal = &Signal{"0", "none"}
	binance.UseTestnet = false
	/*var (
		apiKey = "dfgMbw39hrY3CeCCadZVbeSkpiFIwadEAtsh4sgyma1ZaUctxb04UhSnWzfqIjHP"
		secretKey = "sqAFFeymUvaboWv1vWzzOjHgVZ9gn3OXhpNZbjDX6xOubqGr9dul4Xzu6eM65eRm"
	)*/
	var (
		apiKey = "059iav4dXTF3hjIOngW7Wx8MyBhHdnNyfK0qb7n8JNG3GM8sorv14gz1GxY3vFIf"
		secretKey = "zOaewGgGWS415uPVlBGEG8eYdIUMER2VZm5TZohcTRLsSsKaTCQDKOXcoLttZ4ly"
	) //real keys
	client = binance.NewClient(apiKey, secretKey)
	updateBalance()

	run()


	//if usdt less than 10usd and btc more than 0.00000193 <- start else: Not enough balance

	//if order status was done then..
	//update balance
	//(price when order was created - 1%) as STOP, LIMIT as 99% and 7 dollars of price when order was created
	//after stop-loss-limit successful placed -> check price btc each 0.4 second and sell order status
	//if order is NOT FINISHED => then AND no SELL signal received right now:
	//if it in case 0.27 => cancel stop-loss-limit //UPD from 08.05 => reduce step to 0.6 and min profit as FROM 0.30 set LIMIT 0.24
	//and place new stop-loss-limit with 100.20% of order buy price, moving this cycle to 20 + 7 + 7 + 7 + 7 and etc
	//for example when price diff goes to 100.34 => place stop-loss-limit as 100.27
	//else start again cycle
	//else wait for a while (update status every 0.4 sec) and cancel order if not filled after 7 sec then
	//start again cycle

}

func run() {
	//go startServer() init server http to receive callbacks with signals
	go watchSignal()
	go startTrading()
	go signalClient()
	go displayBalance()
	go showNewPrice()
	b := make(chan struct{})
	<-b
}
func (s *Signal) asByte() []byte {
	marshal, err := json.Marshal(s)
	if err != nil {
		log.Println("Err Marshal?")
		return nil
	}
	return marshal
}

func signalClient() {
	var mutex = &sync.Mutex{}
	for {
		resp, err := grequests.Get("http://"+config.SignalServer, nil)
		// You can modify the request by passing an optional RequestOptions struct

		if err != nil {
			log.Println("Unable to make request: ", err)
		} else {
			atoi, err := strconv.ParseInt(resp.Header.Get("X-Ts"), 10, 64)
			if err != nil {
				log.Println("err convertiang atoi: ", err)
			} else {
				if lastSignalReceived < atoi {
					mutex.Lock()
					lastSignalReceived = atoi
					wasLastSignalSuccessful = false
					mutex.Unlock()
					signalChannel <- resp.Bytes()
				}
			}
		}

		err = resp.Close()
		if err != nil {
			log.Println("Unable to make request CLOSE: ", err)
		}
		time.Sleep(5 * time.Second)
	}
}

func startTrading() {
	log.Println(signal)
	for {
		if signal.TypeSignal == "buy" && wasLastSignalSuccessful == false { // and was one of all signals signal.Step == 60 - buy <- this is next strategy
			//just create global signal60min which lead way
			orders := getOpenOrders()
			for _, order := range orders {
				log.Println(order)
				if order.Side == binance.SideTypeSell {
					err := cancelOrder(order.OrderID)
					if err != nil {
						log.Println("Cancelling order sell after receiving signal BUY ERR")
					}
				}
			}
			tryCycle()
			//after cycle compare balance and if it goes after 8 transactions to minus -> stop and log this data
			time.Sleep(8 * time.Second)
		} else if signal.TypeSignal == "sell" {
			//check for open orders sell and sell by market price
			orders := getOpenOrders()
			for _, order := range orders {
				log.Println("Sell canceling order:", order)
				if order.Side == binance.SideTypeSell {
					regenerateOrderSell(order.OrderID)
				}
				if order.Side == binance.SideTypeBuy {
					err := cancelOrder(order.OrderID)
					if err != nil {
						log.Println(err)
						log.Println("SELL BUY CANCEL")
					}
				}
			}
			updateBalance()
			_ = generateMarketSellOrder(btcAmount)
		}
		time.Sleep(5 * time.Second)
	}
}

func showNewPrice() {
	for {
		log.Println("Current btc price:", getCurrentPriceBTC())
		time.Sleep(1 * time.Minute)
	}
}

func regenerateOrderSell(orderId int64) {
	err := cancelOrder(orderId)
	if err != nil {
		log.Println(err)
		log.Println("SELL CANCELING ORDER ERROR")
	}
	updateBalance()
	_ = generateMarketSellOrder(btcAmount)
	if err != nil {
		log.Println(err)
		log.Println("SELL CANCELING MARKET PRICE ERROR")
	}
}

func getOpenOrders() []*binance.Order{
	do, err := client.NewListOpenOrdersService().Symbol(symbol).Do(context.Background())
	if err != nil {
		log.Println(err)
		log.Println("OPEN ORDER ERR")
	}
	return do
}

func checkOrderState(orderSellLimitInfo **binance.Order) {
	for {
		select {
		case <-quit:
			log.Println("finished loop with", *orderSellLimitInfo)
			return
		default:
			log.Println("Updated sell order", *orderSellLimitInfo)
			if *orderSellLimitInfo != nil {
				if (*orderSellLimitInfo).Status != binance.OrderStatusTypeFilled {
					log.Println("It is not filled yet")
				}
			}
			time.Sleep(30 * time.Second)
		}
	}
}


func performMainActions() {
	price := getCurrentPriceBTC()
	stopPrice := percent.PercentFloat(config.StopLossPercent, price)
	sellPrice := stopPrice - 3 //magic coefficient
	orderSellLimit, err := generateStopLossOrder(stopPrice, sellPrice, btcAmount)
	time.Sleep(5 * time.Second)


	log.Println("Sell limit:", orderSellLimit)

	if err != nil {
		log.Println("SELL ERROR")
		log.Println(err)
	} else {
		log.Println("Trying to count diff")
		lastDiff := config.MinDiff
		orderSellLimitInfo := updateOrderInfo(orderSellLimit.OrderID)
		if orderSellLimitInfo == nil {
			for i := 0; i < 15; i++ {
				log.Println("Trying to fix nil order", orderSellLimit)
				orderSellLimitInfo = updateOrderInfo(orderSellLimit.OrderID)
				if orderSellLimitInfo != nil {
					log.Println("Fix success", orderSellLimit)
					break
				} else {
					time.Sleep(200 * time.Millisecond)
				}
			}
		}

		go checkOrderState(&orderSellLimitInfo)

		for {
			//problem v tom, 4to ono otmenyaet order, i v funcii vizivaet nil
			orderSellLimitInfo = updateOrderInfo(orderSellLimit.OrderID)
			if orderSellLimitInfo == nil {
				for i := 0; i < 30; i++ {
					log.Println("Trying to fix nil order 1", orderSellLimit)
					orderSellLimitInfo = updateOrderInfo(orderSellLimit.OrderID)
					if orderSellLimitInfo != nil {
						log.Println("Fix success 1", orderSellLimit)
						break
					} else {
						time.Sleep(100 * time.Millisecond)
					}
				}
			}
			if orderSellLimitInfo.Status == binance.OrderStatusTypeFilled { //POSSIBLE ERROR IF IT IS IN FILLING STATUS
				log.Println("Break 1")
				wasLastSignalSuccessful = true
				break
			} else if orderSellLimitInfo.Status == binance.OrderStatusTypePartiallyFilled || signal.TypeSignal == "sell" {
				log.Println("Break 2")
				if orderSellLimitInfo.Status == binance.OrderStatusTypePartiallyFilled {
					log.Println("Extra sell: reason partially filled in action")
				}
				if signal.TypeSignal == "sell" {
					log.Println("Extra sell: received sell signal")
				}
				err = cancelOrder(orderSellLimitInfo.OrderID)
				if err != nil {
					log.Println("ERR sell wtf: ", err)
				}
				_ = generateMarketSellOrder(btcAmount)
				wasLastSignalSuccessful = true
				break
			} else {
				newPriceBTC := getCurrentPriceBTC()
				diff := percent.ChangeFloat(price, newPriceBTC) //12
				if diff - lastDiff >= detectFloatDiff(diff) { //12 - 12 = 0 > 0.8; 24 - 12 = 12 > 0.8; 25 - 16 >= 12
					lastDiff = diff - detectFloatDiff(diff) //24 - 0.8 = 16
					err = cancelOrder(orderSellLimitInfo.OrderID)
					if err != nil {
						log.Println("CANCEL ORDER")
						log.Println(err)
					}
					newStopPrice := percent.PercentFloat(100 + lastDiff, price)
					newSellPrice := newStopPrice - 3 //magic coefficient
					orderSellLimitNew, err := generateStopLossOrder(newStopPrice, newSellPrice, btcAmount)
					if err != nil {
						log.Println("Error new stop Limit: ", err)
					} else {
						orderSellLimit = orderSellLimitNew
					}
					log.Println("new diff: ", lastDiff, newStopPrice)
				}
			}
		}
	}

}

func detectFloatDiff(diff float64) float64{
	switch {
	case diff > config.MinDiff && diff <= config.MinDiff + 0.08:
		return 0.10
	case diff > config.MinDiff + 0.08 && diff <= config.MinDiff + 0.20:
		return 0.14
	case diff > config.MinDiff + 0.20 && diff <= config.MinDiff + 0.40:
		return 0.20
	case diff > config.MinDiff + 0.40 && diff <= config.MinDiff + 0.70:
		return 0.28
	case diff > config.MinDiff + 0.70 && diff <= config.MinDiff + 1.2:
		return 0.40
	case diff > config.MinDiff + 1.2:
		return 0.60
	}
	return 0.08
}

func tryBuyBTC() {
	price := getCurrentPriceBTC()
	reducedPrice := percent.PercentFloat(config.TryBuyPrice, price)
	WTB := percent.PercentFloat(99.9, usdAmount) / reducedPrice

	reducedPriceString := strconv.FormatFloat(reducedPrice, 'f', 2, 64)
	WTBString := strconv.FormatFloat(WTB, 'f', 6, 64)
	lastFilled := 0.0
	orderCreated := createOrderForBuy(WTBString, reducedPriceString) // if we have more than 10 dollars
	if orderCreated != nil {
		failure := 0
		for {
			time.Sleep(400 * time.Millisecond)
			order := updateOrderInfo(orderCreated.OrderID) //todo if we have less than 10.3 dollar -> check btc if we got more than xxxxxx then skip to next
			if order != nil { // order != nil || btcAmount > 0.0001
				if order.Status == binance.OrderStatusTypeFilled {
					log.Println(order)
					break
				} else {
					log.Println("Log: Order status not filled ", failure)
					log.Println(order)
					if order.Status == binance.OrderStatusTypePartiallyFilled {
						orderCum, err := strconv.ParseFloat(order.CummulativeQuoteQuantity, 64)
						if err != nil {
							log.Println(err)
							log.Println("CUM ERROR")
						}
						if lastFilled < orderCum {
							lastFilled = orderCum
							failure = 0
						} else {
							failure = failure + 1
						}
					} else {
						failure = failure + 1
					}
				}
			} else {
				log.Println("ERROR update order")
				log.Println(order)
				log.Println(orderCreated)
				failure = failure + 1
			}
			if failure >= 25 {
				order = updateOrderInfo(orderCreated.OrderID)
				if order == nil {
					log.Println("WTF order error", orderCreated)
					break
				}
				if order.Status != binance.OrderStatusTypePartiallyFilled && order.Status != binance.OrderStatusTypeFilled {
					err := cancelOrder(orderCreated.OrderID)
					if err != nil {
						log.Println("FATAL ERROR cancel order")
						log.Println(order)
						log.Println(orderCreated)
					}
					//send to channel NEED RESTART TASK
					break
				} else {
					failure = 0
				}
			}
		}
	}
}

func tryCycle() {
	updateBalance()
	//MAKE SURE TO SEND PERCENT OF FLOAT IS NOT SENDING ZERO AS TOTAL! CAUSE TO PANIC

	//if buy received:
	//act all cycle
	//else sell received: cancel all orders and sell via market price btc if exists
	weHaveMoney := usdAmount > 10.2
	if weHaveMoney {
		log.Println("We have money ", usdAmount, "Buying...")
		tryBuyBTC()
	}
	log.Println("Actioning...")
	updateBalance()
	performMainActions()

	log.Println("end cycle")
	quit <- true

}

func displayBalance() {
	for {
		log.Println("usd: ", usdAmount)
		log.Println("btc: ", btcAmount)
		time.Sleep(1 * time.Minute)
	}
}

func generateMarketSellOrder(btcAmount float64) *binance.CreateOrderResponse {
	if btcAmount > 0.000001 {
		btcAmountString := strconv.FormatFloat(btcAmount, 'f', 6, 64)

		fastSell, err := client.NewCreateOrderService().Symbol("BTCUSDT").
			Side(binance.SideTypeSell).Type(binance.OrderTypeMarket).Quantity(btcAmountString).Do(context.Background())
		if err != nil {
			log.Println(err)
			log.Println("fastsell error")
			log.Println(fastSell)
		}
		return fastSell
	}
	return nil
}

func generateStopLossOrder(stopPriceString, sellPriceString, btcAmount float64) (*binance.CreateOrderResponse, error){
	newStopPriceString := strconv.FormatFloat(stopPriceString, 'f', 2, 64)
	newSellPriceString := strconv.FormatFloat(sellPriceString, 'f', 2, 64)
	btcAmountString := strconv.FormatFloat(btcAmount, 'f', 6, 64)

	orderSellLimit, err := client.NewCreateOrderService().Symbol("BTCUSDT").
		Side(binance.SideTypeSell).Type(binance.OrderTypeStopLossLimit).StopPrice(newStopPriceString).
		TimeInForce(binance.TimeInForceTypeGTC).Quantity(btcAmountString).
		Price(newSellPriceString).Do(context.Background())
	return orderSellLimit, err
}

func watchSignal() {
	var mutex = &sync.Mutex{}
	for {
		select {
		case data := <-signalChannel:

			var receivedSignal *Signal
			err := json.Unmarshal(data, &receivedSignal)
			if err != nil {
				log.Println(err)
				log.Println("Unmarshal signal")
				log.Println(data)
			} else {
				mutex.Lock()
				signal = receivedSignal
				//if SELL received -> send to channel SELL NOW BY MARKET PRICE
				mutex.Unlock()
				log.Println("Received signal ", signal)
			}

		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func createOrderForBuy(WTB string, price string) *binance.CreateOrderResponse {
	order, err := client.NewCreateOrderService().Symbol("BTCUSDT").
		Side(binance.SideTypeBuy).Type(binance.OrderTypeLimit).
		TimeInForce(binance.TimeInForceTypeGTC).Quantity(WTB).
		Price(price).Do(context.Background())
	if err != nil {
		log.Println(err)
		log.Println("ORDER CREATED ERROR")
		//return nil
	}

	return order
}

func cancelOrder(orderId int64) error {
	_, err := client.NewCancelOrderService().Symbol("BTCUSDT").
		OrderID(orderId).Do(context.Background())

	return err
}

func updateOrderInfo(orderId int64) *binance.Order {
	order, err := client.NewGetOrderService().Symbol("BTCUSDT").
		OrderID(orderId).Do(context.Background())
	if err != nil {
		log.Println("UPD XS", err)
		//return nil
	}

	return order
}

func updateBalance() {
	res, err := client.NewGetAccountService().Do(context.Background())
	if err != nil {
		log.Println(err)
		log.Println("UPDATE BALANCE")
		return
	}
	for _, value := range res.Balances {
		if value.Asset == "BTC" {
			tmpBtcAmount, err := strconv.ParseFloat(value.Free, 64)
			btcAmount = tmpBtcAmount - 0.000001
			if err != nil {
				log.Println(err)
				log.Println("GetBalance BTC")
			}
		}
		if value.Asset == "USDT" {
			usdAmount, err = strconv.ParseFloat(value.Free, 64)
			if err != nil {
				log.Println(err)
				log.Println("GetBalance USD")
			}
		}
	}
}

func getCurrentPriceBTC() float64 {
	prices, err := client.NewListPricesService().Symbol("BTCUSDT").Do(context.Background())
	if err != nil {
		log.Println(err)
		log.Println("getCurrentPrice error")
	}
	price, err := strconv.ParseFloat(prices[0].Price, 64)
	if err != nil {
		log.Println(err)
		log.Println("getCurrentPrice error strconv")
	}
	return price
}
