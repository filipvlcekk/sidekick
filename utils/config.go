/*
Copyright © 2024 Mahmoud Mousa <m.mousa@hey.com>

Licensed under the GNU GPL License, Version 3.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
https://www.gnu.org/licenses/gpl-3.0.en.html

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package utils

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func GetSidekickConfigFromCmdContext(cmd *cobra.Command) (*SidekickConfig, error) {
	config, ok := cmd.Context().Value("config").(*SidekickConfig)

	if !ok {
		return nil, fmt.Errorf("config not found in cmd context. This is likely a bug in the root cmd initialization.")
	}

	return config, nil
}

func (c *SidekickConfig) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	return os.WriteFile(path, data, os.ModePerm)
}

func (c *SidekickConfig) Print() error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	_, err = os.Stdout.Write(data)
	return err
}

func (c *SidekickConfig) FindContext(name string) (SidekickContext, error) {
	for _, ctx := range c.Contexts {
		if ctx.Name == name {
			return ctx, nil
		}
	}
	return SidekickContext{}, fmt.Errorf("context '%s' not found", name)
}

func (c *SidekickConfig) FindServer(name string) (SidekickServer, error) {
	for _, s := range c.Servers {
		if s.Name == name {
			return s, nil
		}
	}
	return SidekickServer{}, fmt.Errorf("server '%s' not found", name)
}

func (c *SidekickConfig) FindServerByContext(ctxName string) (SidekickServer, error) {
	ctx, err := c.FindContext(ctxName)
	if err != nil {
		return SidekickServer{}, err
	}

	return c.FindServer(ctx.Server)
}

func (c *SidekickConfig) AddOrReplaceContext(ctx SidekickContext) {
	idx := -1
	for i, elem := range c.Contexts {
		if elem.Name == ctx.Name {
			idx = i
			break
		}
	}

	if idx == -1 {
		c.Contexts = append(c.Contexts, ctx)
	} else {
		c.Contexts[idx] = ctx
	}
}

func (c *SidekickConfig) AddOrReplaceServer(s SidekickServer) {
	idx := -1
	for i, elem := range c.Servers {
		if elem.Name == s.Name {
			idx = i
			break
		}
	}

	if idx == -1 {
		c.Servers = append(c.Servers, s)
	} else {
		c.Servers[idx] = s
	}
}
