package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lazy-void/primitive-bot/pkg/menu"
	"github.com/lazy-void/primitive-bot/pkg/sessions"
	"github.com/lazy-void/primitive-bot/pkg/tg"
)

func (app *application) listenAndServe() {
	go app.worker()

	offset := int64(0)
	for {
		updates, err := app.bot.GetUpdates(offset)
		if err != nil {
			app.errorLog.Print(err)
			continue
		}

		numUpdates := len(updates)
		if numUpdates == 0 {
			continue
		}

		for _, u := range updates {
			if u.Message.MessageID > 0 {
				app.infoLog.Printf("Got message with text '%s' from the user '%s' with ID '%d'",
					u.Message.Text, u.Message.From.FirstName, u.Message.From.ID)
				go app.processMessage(u.Message)
				continue
			}

			app.infoLog.Printf("Got callback query with data '%s' from the user '%s' with ID '%d'",
				u.CallbackQuery.Data, u.CallbackQuery.From.FirstName, u.CallbackQuery.From.ID)
			go app.processCallbackQuery(u.CallbackQuery)
		}

		offset = updates[numUpdates-1].UpdateID + 1
	}
}

func (app *application) worker() {
	for {
		// If we delete the operation from the queue with Dequeue
		// then if user will use command '/status', we won't be able to
		// send him any information about this operation (it is not in the
		// queue so we have no idea if it exists or not)
		op, ok := app.queue.Peek()
		if !ok {
			time.Sleep(1 * time.Second)
			continue
		}

		start := time.Now()
		app.infoLog.Printf("Creating from '%s' for chat '%d': count=%d, mode=%d, alpha=%d, repeat=%d, resolution=%d, extension=%s",
			op.ImgPath, op.ChatID, op.Config.Iterations, op.Config.Shape, op.Config.Alpha, op.Config.Repeat, op.Config.OutputSize, op.Config.Extension)

		// create primitive
		outputPath := fmt.Sprintf("%s/%d_%d.%s", app.outDir, op.ChatID, start.Unix(), op.Config.Extension)
		err := op.Config.Create(op.ImgPath, outputPath)
		if err != nil {
			app.serverError(op.ChatID, err)
			return
		}
		app.infoLog.Printf("Finished creating '%s' for chat '%d'; Output: '%s'; Time: %.1f seconds",
			filepath.Base(op.ImgPath), op.ChatID, filepath.Base(outputPath), time.Since(start).Seconds())

		// send output to the user
		err = app.bot.SendDocument(op.ChatID, outputPath)
		if err != nil {
			app.serverError(op.ChatID, err)
			return
		}
		app.infoLog.Printf("Sent result '%s' to the chat '%d'", filepath.Base(outputPath), op.ChatID)

		// remove operation from the queue
		app.queue.Dequeue()
	}
}

func (app *application) processMessage(m tg.Message) {
	if m.Photo != nil {
		app.processPhoto(m)
		return
	}

	// Handle user input if they are inside input form
	s, ok := app.sessions.Get(m.From.ID)
	if ok && s.InChan != nil {
		s.InChan <- m
		return
	}

	if m.Text == "/status" {
		operations, positions := app.queue.GetOperations(s.UserID)
		if len(operations) == 0 {
			_, err := app.bot.SendMessage(m.Chat.ID, statusEmptyMessage)
			if err != nil {
				app.serverError(m.Chat.ID, err)
			}
			return
		}

		for i, op := range operations {
			_, err := app.bot.SendMessage(m.Chat.ID, app.createStatusMessage(op.Config, positions[i]))
			if err != nil {
				app.serverError(m.Chat.ID, err)
				return
			}
		}
	}

	// Send help message
	_, err := app.bot.SendMessage(m.Chat.ID, helpMessage)
	if err != nil {
		app.serverError(m.Chat.ID, err)
	}
}

func (app *application) processPhoto(m tg.Message) {
	// If we already have session - delete it's menu
	s, ok := app.sessions.Get(m.From.ID)
	if ok {
		err := app.bot.DeleteMessage(s.UserID, s.MenuMessageID)
		if err != nil {
			app.serverError(m.Chat.ID, err)
			return
		}
	}

	path, err := app.downloadPhoto(m.Photo)
	if err != nil {
		app.serverError(m.Chat.ID, err)
		return
	}

	// Create session
	msg, err := app.bot.SendMessage(m.Chat.ID, menu.RootActivityTmpl.Text, menu.RootActivityTmpl.Keyboard)
	if err != nil {
		app.serverError(m.Chat.ID, err)
		return
	}
	app.sessions.Set(m.From.ID, sessions.NewSession(m.From.ID, msg.MessageID, path))
}

func (app *application) downloadPhoto(photos []tg.PhotoSize) (string, error) {
	// Choose smallest image with dimensions >= 256
	var file tg.PhotoSize
	for _, photo := range photos {
		file = photo
		if photo.Width >= 256 && photo.Height >= 256 {
			break
		}
	}
	if file.FileID == "" {
		return "", fmt.Errorf("no image files in %v", photos)
	}

	path := fmt.Sprintf("%s/%s.jpg", app.inDir, file.FileUniqueID)
	// Download the file only if we don't have it
	if _, err := os.Stat(path); os.IsNotExist(err) {
		img, err := app.bot.DownloadFile(file.FileID)
		if err != nil {
			return "", fmt.Errorf("couldn't download image: %s", err)
		}

		if err := os.WriteFile(path, img, 0644); err != nil {
			return "", fmt.Errorf("couldn't save image: %s", err)
		}
	}

	return path, nil
}

func (app *application) processCallbackQuery(q tg.CallbackQuery) {
	defer app.bot.AnswerCallbackQuery(q.ID, "")

	s, ok := app.sessions.Get(q.From.ID)
	if !ok || q.Message.MessageID != s.MenuMessageID {
		err := app.bot.DeleteMessage(q.Message.Chat.ID, q.Message.MessageID)
		if err != nil {
			app.errorLog.Printf("Deleting message error: %s", err)
		}
		return
	}

	var num int
	var slug string
	switch {
	case match(q.Data, menu.RootActivityCallback):
		app.showRootMenuActivity(s)
	case match(q.Data, menu.CreateButtonCallback):
		app.handleCreateButton(s)
	case match(q.Data, menu.ShapesActivityCallback):
		app.showShapesMenuActivity(s)
	case match(q.Data, menu.ShapesButtonCallback, &num):
		app.handleShapesButton(s, num)
	case match(q.Data, menu.IterActivityCallback):
		app.showIterMenuActivity(s)
	case match(q.Data, menu.IterButtonCallback, &num):
		app.handleIterButton(s, num)
	case match(q.Data, menu.IterInputCallback):
		app.handleIterInput(s)
	case match(q.Data, menu.RepActivityCallback):
		app.showRepMenuActivity(s)
	case match(q.Data, menu.RepButtonCallback, &num):
		app.handleRepButton(s, num)
	case match(q.Data, menu.AlphaActivityCallback):
		app.showAlphaMenuActivity(s)
	case match(q.Data, menu.AlphaButtonCallback, &num):
		app.handleAlphaButton(s, num)
	case match(q.Data, menu.AlphaInputCallback):
		app.handleAlphaInput(s)
	case match(q.Data, menu.ExtActivityCallback):
		app.showExtMenuActivity(s)
	case match(q.Data, menu.ExtButtonCallback, &slug):
		app.handleExtButton(s, slug)
	case match(q.Data, menu.SizeActivityCallback):
		app.showSizeMenuActivity(s)
	case match(q.Data, menu.SizeButtonCallback, &num):
		app.handleSizeButton(s, num)
	case match(q.Data, menu.SizeInputCallback):
		app.handleSizeInput(s)
	}
}