// Code generated by templ - DO NOT EDIT.

// templ: version: v0.3.833
package dashboard

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import templruntime "github.com/a-h/templ/runtime"

import "orisun/admin/templates"
import "fmt"

type DashboardDetails struct {
	UserCount    uint32 `json:"user_count"`
	CatchupCount int    `json:"catchup_count"`
	PubsubCount  int    `json:"pubsub_count"`
	EventCount   int    `json:"event_count"`
	StreamCount  int    `json:"stream_count"`
	SystemStatus int    `json:"system_status"`
}

const UserCountId string = "userCount"
const catchupCountId = "catchupCount"
const pubsubCountId = "pubsubCount"
const eventCountId = "eventCount"
const streamCountId = "streamCount"
const systemStatusId = "systemStatus"

func UserCountFragement(userCount uint32) templ.Component {
	return templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
		templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context
		if templ_7745c5c3_CtxErr := ctx.Err(); templ_7745c5c3_CtxErr != nil {
			return templ_7745c5c3_CtxErr
		}
		templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
		if !templ_7745c5c3_IsBuffer {
			defer func() {
				templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
				if templ_7745c5c3_Err == nil {
					templ_7745c5c3_Err = templ_7745c5c3_BufErr
				}
			}()
		}
		ctx = templ.InitializeContext(ctx)
		templ_7745c5c3_Var1 := templ.GetChildren(ctx)
		if templ_7745c5c3_Var1 == nil {
			templ_7745c5c3_Var1 = templ.NopComponent
		}
		ctx = templ.ClearChildren(ctx)
		templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 1, "<p id=\"")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		var templ_7745c5c3_Var2 string
		templ_7745c5c3_Var2, templ_7745c5c3_Err = templ.JoinStringErrs(UserCountId)
		if templ_7745c5c3_Err != nil {
			return templ.Error{Err: templ_7745c5c3_Err, FileName: `admin/slices/dashboard/dashboard.templ`, Line: 23, Col: 20}
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(templ_7745c5c3_Var2))
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 2, "\" class=\"text-3xl font-bold mt-2\" data-bind-userCount>")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		var templ_7745c5c3_Var3 string
		templ_7745c5c3_Var3, templ_7745c5c3_Err = templ.JoinStringErrs(fmt.Sprint(userCount))
		if templ_7745c5c3_Err != nil {
			return templ.Error{Err: templ_7745c5c3_Err, FileName: `admin/slices/dashboard/dashboard.templ`, Line: 23, Col: 98}
		}
		_, templ_7745c5c3_Err = templ_7745c5c3_Buffer.WriteString(templ.EscapeString(templ_7745c5c3_Var3))
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 3, "</p>")
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		return nil
	})
}

func Dashboard(currentPath string, data DashboardDetails) templ.Component {
	return templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
		templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context
		if templ_7745c5c3_CtxErr := ctx.Err(); templ_7745c5c3_CtxErr != nil {
			return templ_7745c5c3_CtxErr
		}
		templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
		if !templ_7745c5c3_IsBuffer {
			defer func() {
				templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
				if templ_7745c5c3_Err == nil {
					templ_7745c5c3_Err = templ_7745c5c3_BufErr
				}
			}()
		}
		ctx = templ.InitializeContext(ctx)
		templ_7745c5c3_Var4 := templ.GetChildren(ctx)
		if templ_7745c5c3_Var4 == nil {
			templ_7745c5c3_Var4 = templ.NopComponent
		}
		ctx = templ.ClearChildren(ctx)
		templ_7745c5c3_Var5 := templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
			templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context
			templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
			if !templ_7745c5c3_IsBuffer {
				defer func() {
					templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
					if templ_7745c5c3_Err == nil {
						templ_7745c5c3_Err = templ_7745c5c3_BufErr
					}
				}()
			}
			ctx = templ.InitializeContext(ctx)
			templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 4, "<div class=\"p-6\" data-on-load=\"@get(&#39;/dashboard&#39;)\"><div class=\"mb-6\"><h1 class=\"text-2xl font-bold text-gray-900\">Dashboard</h1></div><div class=\"grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6\"><sl-card><a href=\"/users\"><div class=\"flex items-center justify-between\"><div><h3 class=\"text-lg font-medium text-gray-900\">Total Users</h3>")
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			templ_7745c5c3_Err = UserCountFragement(data.UserCount).Render(ctx, templ_7745c5c3_Buffer)
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 5, "</div><sl-icon name=\"people\" style=\"font-size: 2rem\"></sl-icon></div></a></sl-card> <sl-card><div class=\"flex items-center justify-between\"><div><h3 class=\"text-lg font-medium text-gray-900\">Catchup Subscriptions</h3><p class=\"text-3xl font-bold mt-2\" data-bind-catchupCount>0</p></div><sl-icon name=\"arrow-repeat\" style=\"font-size: 2rem\"></sl-icon></div></sl-card> <sl-card><div class=\"flex items-center justify-between\"><div><h3 class=\"text-lg font-medium text-gray-900\">PubSub Subscriptions</h3><p class=\"text-3xl font-bold mt-2\" data-bind-pubsubCount>0</p></div><sl-icon name=\"broadcast\" style=\"font-size: 2rem\"></sl-icon></div></sl-card> <sl-card><div class=\"flex items-center justify-between\"><div><h3 class=\"text-lg font-medium text-gray-900\">Total Events</h3><p class=\"text-3xl font-bold mt-2\" data-bind-eventCount>0</p></div><sl-icon name=\"database\" style=\"font-size: 2rem\"></sl-icon></div></sl-card> <sl-card><div class=\"flex items-center justify-between\"><div><h3 class=\"text-lg font-medium text-gray-900\">Active Streams</h3><p class=\"text-3xl font-bold mt-2\" data-bind-streamCount>0</p></div><sl-icon name=\"activity\" style=\"font-size: 2rem\"></sl-icon></div></sl-card> <sl-card><div class=\"flex items-center justify-between\"><div><h3 class=\"text-lg font-medium text-gray-900\">System Status</h3><div class=\"flex items-center mt-2\"><sl-badge variant=\"success\" data-bind-systemStatus>Healthy</sl-badge></div></div><sl-icon name=\"check-circle\" style=\"font-size: 2rem\"></sl-icon></div></sl-card></div></div>")
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			return nil
		})
		templ_7745c5c3_Err = templates.Admin(currentPath, "Dashboard - Orisun Admin").Render(templ.WithChildren(ctx, templ_7745c5c3_Var5), templ_7745c5c3_Buffer)
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		return nil
	})
}

var _ = templruntime.GeneratedTemplate
