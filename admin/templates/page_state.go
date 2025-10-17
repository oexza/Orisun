package templates

type UsersPageState struct {
	Users []User
}

type DashboardPageState struct {
	
}

type LoginPageState struct {
	
}

type PageState struct {
	UsersPageState UsersPageState
	DashboardPageState DashboardPageState
	LoginPageState LoginPageState
}