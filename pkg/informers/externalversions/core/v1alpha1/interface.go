/*
Copyright 2020 The Tilt Dev Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	internalinterfaces "github.com/tilt-dev/tilt/pkg/informers/externalversions/internalinterfaces"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// Cmds returns a CmdInformer.
	Cmds() CmdInformer
	// FileWatches returns a FileWatchInformer.
	FileWatches() FileWatchInformer
}

type version struct {
	factory          internalinterfaces.SharedInformerFactory
	namespace        string
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory, namespace string, tweakListOptions internalinterfaces.TweakListOptionsFunc) Interface {
	return &version{factory: f, namespace: namespace, tweakListOptions: tweakListOptions}
}

// Cmds returns a CmdInformer.
func (v *version) Cmds() CmdInformer {
	return &cmdInformer{factory: v.factory, tweakListOptions: v.tweakListOptions}
}

// FileWatches returns a FileWatchInformer.
func (v *version) FileWatches() FileWatchInformer {
	return &fileWatchInformer{factory: v.factory, tweakListOptions: v.tweakListOptions}
}
