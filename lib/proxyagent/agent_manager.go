package proxyagent

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type AgentConfig struct {
	DisableProxy    bool
	GenerateRSAKey  bool
	ValidPrincipals []string
	VaultSigningUrl string
}

func SetupAgent(config AgentConfig) (agent.Agent, error) {
	var sshAgent agent.Agent
	var err error

	if config.VaultSigningUrl != "" {
		sshAgent, err = setupSigningAgent(config.VaultSigningUrl, config.ValidPrincipals)
	} else {
		sshAgent = agent.NewKeyring()
	}

	upstreamAuthSock := os.Getenv("SSH_AUTH_SOCK")
	if !config.DisableProxy && upstreamAuthSock != "" {
		var conn net.Conn
		conn, err = net.Dial("unix", upstreamAuthSock)
		if err != nil {
			return nil, err
		}

		upstream := agent.NewClient(conn)
		sshAgent = NewProxyKeyring(sshAgent, upstream)
	}

	if config.GenerateRSAKey {
		err = generateAndAddKey(sshAgent)
		if err != nil {
			return nil, err
		}
	}
	return sshAgent, nil
}

func setupSigningAgent(vaultSigningUrl string, validPrincipals []string) (agent.Agent, error) {
	if vaultAddr := os.Getenv("VAULT_ADDR"); vaultAddr != "" {
		vaultURL, vaultURLErr := url.Parse(vaultAddr)
		signingURL, signingURLErr := url.Parse(vaultSigningUrl)

		if vaultURLErr == nil && signingURLErr == nil {
			vaultSigningUrl = vaultURL.ResolveReference(signingURL).String()
		}
	}

	validPrincipalsString := strings.Join(validPrincipals, ",")
	return NewSigningKeyring(vaultSigningUrl, validPrincipalsString)
}

type KeyPair struct {
	PrivateKey string
	PublicKey  string
}

func generateAndAddKey(keyring agent.Agent) error {
	fmt.Println("Generating an RSA key...")
	keyPair, err := generateRSAKeyPair()
	if err != nil {
		return err
	}

	parsedKey, err := ssh.ParseRawPrivateKey([]byte(keyPair.PrivateKey))
	if err != nil {
		return err
	}

	return keyring.Add(agent.AddedKey{
		PrivateKey: parsedKey,
		Comment:    "ssh-proxy-agent-generated-key",
	})
}

func generateRSAKeyPair() (*KeyPair, error) {
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}

	var private bytes.Buffer
	err = pem.Encode(&private, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err != nil {
		return nil, err
	}

	pubkey, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}

	return &KeyPair{
		PrivateKey: string(private.Bytes()),
		PublicKey:  string(ssh.MarshalAuthorizedKey(pubkey)),
	}, nil
}
