package main

import "testing"

func TestHTTP3FlagsParseForStartAndRestart(t *testing.T) {
	for _, verb := range []string{"start", "restart"} {
		t.Run(verb, func(t *testing.T) {
			cli, parser := newTestParser(t)
			_, err := parser.Parse([]string{verb, "--http3-addr", "0.0.0.0:12581", "--http3-cert", "cert.pem", "--http3-key", "key.pem", "--no-daemon"})
			if err != nil {
				t.Fatal(err)
			}
			flags := cli.Start.ServerFlagsKong
			if verb == "restart" {
				flags = cli.Restart.ServerFlagsKong
			}
			if flags.HTTP3Addr != "0.0.0.0:12581" || flags.HTTP3Cert != "cert.pem" || flags.HTTP3Key != "key.pem" {
				t.Fatalf("flags lost: %+v", flags)
			}
		})
	}
}
