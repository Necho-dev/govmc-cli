package tui

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/Necho-dev/govmc-cli/internal/workflow"
	"github.com/rivo/tview"
)

type tuiFileTransferFunc func(context.Context, string, string, bool, bool) (any, error)

func openTUIFileTransfer(ctx context.Context, app *tview.Application, pages *tview.Pages, t target.Target, ds, src string, copy bool, l language, submitted func(tasks.Task), destinationStores ...string) {
	transfer := func(ctx context.Context, dstDS, dst string, dry, confirm bool) (any, error) {
		c, err := t.Dial(ctx)
		if err != nil {
			return nil, err
		}
		return workflow.CopyMove(ctx, tasks.From(ctx), c, t.Name, c.Endpoint(), ds, src, dst, copy, dry, confirm, "", func() { target.Close(c) }, dstDS)
	}
	openTUIFileTransferForm(ctx, app, pages, t, ds, src, copy, l, transfer, submitted, destinationStores...)
}

func openTUIFileTransferForm(ctx context.Context, app *tview.Application, pages *tview.Pages, t target.Target, ds, src string, copy bool, l language, transfer tuiFileTransferFunc, submitted func(tasks.Task), destinationStores ...string) {
	previous := app.GetFocus()
	title := l.Text("files.move")
	if copy {
		title = l.Text("files.copy")
	}
	form := tview.NewForm().SetLabelColor(tuiMuted).SetFieldBackgroundColor(tuiHeader).SetFieldTextColor(tuiText).SetButtonBackgroundColor(tuiSelect).SetButtonTextColor(tuiText)
	form.SetBackgroundColor(tuiHeader)
	initial := ""
	if copy {
		initial = src + ".copy"
	}
	names := []string{ds}
	for _, name := range destinationStores {
		if name != ds {
			names = append(names, name)
		}
	}
	form.AddDropDown(l.Text("Destination datastore"), names, 0, nil)
	form.AddInputField(l.Text("Destination path"), initial, 0, nil, nil)
	message := tuiTextView().SetText(fmt.Sprintf("%s  %s\n[%s] %s", t.Name, tuiEndpoint(t), ds, src))
	message.SetBackgroundColor(tuiHeader)
	busy := false
	close := func() {
		if !busy {
			pages.RemovePage("file-transfer")
			app.SetFocus(previous)
		}
	}
	form.AddButton(l.Text("Preview changes"), func() {
		if busy {
			return
		}
		dst := strings.TrimSpace(form.GetFormItem(1).(*tview.InputField).GetText())
		index, _ := form.GetFormItem(0).(*tview.DropDown).GetCurrentOption()
		dstDS := names[index]
		if _, _, err := esxi.NormalizeTransferPaths(src, dst, ds != dstDS); err != nil {
			message.SetText(l.Text("Invalid source or destination path"))
			return
		}
		busy = true
		message.SetText(l.Text("Loading"))
		go func() {
			previewCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			out, err := transfer(previewCtx, dstDS, dst, true, false)
			cancel()
			if ctx.Err() != nil {
				return
			}
			app.QueueUpdateDraw(func() {
				busy = false
				if err != nil {
					message.SetText(tview.Escape(err.Error()))
					app.SetFocus(form)
					return
				}
				plan, ok := out.(*esxi.StepResult)
				if !ok {
					message.SetText(l.Text("Could not preview file transfer"))
					return
				}
				detail, ok := plan.Detail.(esxi.FileTransferPlan)
				if !ok {
					message.SetText(l.Text("Could not preview file transfer"))
					return
				}
				summary := fmt.Sprintf("%s  %s\n%s  %s\n%s  %s\n%s  %s\n\n%s", l.Text("Profile"), t.Name+" / "+tuiEndpoint(t), l.Text("Datastore"), detail.Datastore+" → "+detail.DestinationDatastore, l.Text("Source path"), detail.Source, l.Text("Destination path"), detail.Destination, l.Text("Destination must not exist. Completed steps are not rolled back on cancellation."))
				showTUIConfirmation(app, pages, "file-transfer-confirm", title, tview.Escape(summary), l.Text("Back"), l.Text("Confirm"), l, func(answer int) {
					if answer != 1 {
						app.SetFocus(form)
						return
					}
					busy = true
					message.SetText(l.Text("Submitting task"))
					go func() {
						submitCtx, stop := context.WithTimeout(ctx, 30*time.Second)
						out, err := transfer(submitCtx, detail.DestinationDatastore, detail.Destination, false, true)
						stop()
						if ctx.Err() != nil {
							return
						}
						app.QueueUpdateDraw(func() {
							busy = false
							if err != nil {
								message.SetText(tview.Escape(err.Error()))
								app.SetFocus(form)
								return
							}
							pages.RemovePage("file-transfer")
							submitted(out.(tasks.Task))
						})
					}()
				})
			})
		}()
	})
	form.AddButton(l.Text("Cancel"), close).SetCancelFunc(close)
	body := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(message, 3, 0, false).AddItem(form, 0, 1, true)
	pages.AddPage("file-transfer", tuiEnvironmentDialog(body, title+" · "+path.Base(src), 82, 20), true, true)
	app.SetFocus(form)
}
