package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/FEATO-org/support-feato-system/config"
	"github.com/FEATO-org/support-feato-system/infrastructure"
	"github.com/FEATO-org/support-feato-system/interfaces"
	"github.com/FEATO-org/support-feato-system/usecase"
	"github.com/bwmarrin/discordgo"
)

func main() {
	log.SetFlags(log.Llongfile)
	if err := run(); err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	dbtx, err := config.NewDB()
	if err != nil {
		return err
	}
	defer dbtx.Close()
	discordToken := os.Getenv("DISCORD_TOKEN")
	guildIDList := strings.Split(os.Getenv("GUILD_IDS"), ",")
	discordConfig := config.NewDiscordConfig()
	p2pEarthquakeConfig := config.NewP2PEarthquake()
	// Keep loading the existing OAuth configuration at startup.
	_ = config.NewOauth2()

	dice := usecase.NewDice()
	character := usecase.NewCharacter(infrastructure.NewCharacterGenerator())
	systemUser := usecase.NewSystemUser(infrastructure.NewSystemUserRepository(dbtx))

	discordCommandInterfaces := interfaces.NewDiscordCommandInterfaces(ctx, dice, character, systemUser)

	discordInterfaces := interfaces.NewDiscordInterfaces(discordCommandInterfaces, guildIDList, *discordConfig)

	// discordへの接続と初期化処理
	dg, err := discordgo.New("Bot " + discordToken)
	if err != nil {
		return fmt.Errorf("create Discord session: %w", err)
	}
	dg.AddHandlerOnce(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
	})
	err = dg.Open()
	if err != nil {
		return fmt.Errorf("open Discord session: %w", err)
	}
	fmt.Println("Bot open connection.  Press CTRL-C to exit.")

	notifier := infrastructure.NewEEWDiscordNotifier(dg, discordConfig.NotifyChannelID)
	p2pEarthquakeInterfaces := interfaces.NewP2PEarthquakeInterfaces(*p2pEarthquakeConfig, ctx, usecase.NewEEW(notifier))

	// コマンドやハンドラーの登録
	discordInterfaces.CreateApplicationCommand(dg)
	discordInterfaces.AddCommandHandler(dg)
	discordInterfaces.AddMessageHandler(dg)
	discordInterfaces.AddGuildLeaveHandler(dg)
	discordInterfaces.AddComponentHandler(dg)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := p2pEarthquakeInterfaces.ReceiveEEW(); err != nil {
			log.Printf("EEW receiver: %v", err)
		}
	}()

	fmt.Println("Bot is now running.")

	<-ctx.Done()
	<-done
	// discordInterfaces.DeleteApplicationCommands(dg)
	dg.Close()
	fmt.Println("Bot is shutdown.")
	return nil
}
