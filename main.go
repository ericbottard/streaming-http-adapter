/*
 * Copyright 2019 the original author or authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"fmt"
	"github.com/projectriff/streaming-http-adapter/pkg/build"
	"github.com/projectriff/streaming-http-adapter/pkg/proxy"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func main() {
	// Lookup the gRPC address where our child process expects to run.
	grpcPort := os.Getenv("GRPC_PORT")
	if grpcPort == "" {
		grpcPort = "8081"
	}

	// Per the http-invoker contract, listen for http traffic on a port defined by PORT
	httpPort := os.Getenv("PORT")
	if httpPort == "" {
		httpPort = "8080"
	}
	if len(os.Args) < 2 {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: %s invoker-command [invoker-args]...\n", os.Args[0])
		os.Exit(1)
	}

	proxy, err := proxy.NewProxy(fmt.Sprintf(":%s", grpcPort), fmt.Sprintf(":%s", httpPort))
	if err != nil {
		panic(err)
	}
	go func() {
		if err := proxy.Run(); err != nil {
			log.Fatalf("error running proxy %v", err)
		}
	}()

	command := exec.Command(os.Args[1], os.Args[2:]...)
	command.Stdout = os.Stdout
	command.Stdin = os.Stdin
	command.Stderr = os.Stderr
	// The following makes sure that our child process sees the GRPC_PORT variable too.
	// It should not care about the PORT variable
	command.Env = os.Environ()

	done := make(chan struct{}, 2)

	go func() {
		fmt.Printf("Starting streaming-http-adapter %v %v%v\n\n", build.Version, build.Gitsha, build.Gitdirty)

		if err := command.Run(); err != nil {
			fmt.Printf("Child process exited with %v\n", err)
		}
		done <- struct{}{}
		if err := proxy.Shutdown(context.Background()); err != nil {
			log.Fatalf("error shuting down proxy server %v", err)
		}
		done <- struct{}{}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGKILL, syscall.SIGTERM)

	go func() {
		// Wait for explicit termination of this adapter
		sig := <-stop

		// Forward the caught signal to our child
		if err := command.Process.Signal(sig); err != nil {
			panic(err)
		}
	}()

	// Wait for both the child and the http server
	<-done
	<-done
}
