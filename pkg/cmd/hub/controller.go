package hub

import (
	"github.com/spf13/cobra"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/open-cluster-management/work/pkg/hub"
	"github.com/open-cluster-management/work/pkg/version"
)

func NewController() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("work-controller", version.Get(), hub.RunControllerManager).
		NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the Work Controller"

	return cmd
}
