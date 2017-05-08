package main
//// Copyright 2016 The prometheus-operator Authors
////
//// Licensed under the Apache License, Version 2.0 (the "License");
//// you may not use this file except in compliance with the License.
//// You may obtain a copy of the License at
////
////     http://www.apache.org/licenses/LICENSE-2.0
////
//// Unless required by applicable law or agreed to in writing, software
//// distributed under the License is distributed on an "AS IS" BASIS,
//// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//// See the License for the specific language governing permissions and
//// limitations under the License.
//
//package main
//
//import (
//	"context"
//	"flag"
//	"fmt"
//	"os"
//	"os/signal"
//	"syscall"
//
//	"github.com/Sirupsen/logrus"
//	"github.com/grapebaba/fabric-operator/pkg1/fabric"
//	"golang.org/x/sync/errgroup"
//)
//
//var (
//	cfg          fabric.Config
//	printVersion bool
//	namespace    string
//	name         string
//)
//
//func init() {
//	flag.BoolVar(&printVersion, "version", false, "Show version and quit")
//	flag.Parse()
//}
//
//func Main() int {
//	namespace = os.Getenv("MY_POD_NAMESPACE")
//	if len(namespace) == 0 {
//		logrus.Fatal("must set env MY_POD_NAMESPACE")
//	}
//	name = os.Getenv("MY_POD_NAME")
//	if len(name) == 0 {
//		logrus.Fatal("must set env MY_POD_NAME")
//	}
//
//	cfg = newControllerConfig()
//	fo, err := fabric.New(cfg)
//	if err != nil {
//		fmt.Fprint(os.Stderr, err)
//		return 1
//	}
//
//	ctx, cancel := context.WithCancel(context.Background())
//	wg, ctx := errgroup.WithContext(ctx)
//
//	wg.Go(func() error { return fo.Run(ctx.Done()) })
//
//	term := make(chan os.Signal)
//	signal.Notify(term, os.Interrupt, syscall.SIGTERM)
//
//	select {
//	case <-term:
//		logrus.Info("Received SIGTERM, exiting gracefully...")
//	case <-ctx.Done():
//	}
//
//	cancel()
//	if err := wg.Wait(); err != nil {
//		logrus.Errorf("Unhandled error received: %v. Exiting...", err)
//		return 1
//	}
//
//	return 0
//}
//
//func main() {
//	os.Exit(Main())
//}
//
//func newControllerConfig() fabric.Config {
//	cfg := fabric.Config{
//		Namespace: namespace,
//		Name:      name,
//	}
//
//	return cfg
//}
