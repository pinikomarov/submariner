package kp_iptables

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/coreos/go-iptables/iptables"
	"github.com/submariner-io/admiral/pkg/log"
	"k8s.io/klog"

	"github.com/submariner-io/submariner/pkg/routeagent-driver/constants"
	"github.com/submariner-io/submariner/pkg/util"
)

func (kp *SyncHandler) createIPTableChains() error {
	ipt, err := iptables.New()
	if err != nil {
		return fmt.Errorf("error initializing iptables: %v", err)
	}

	klog.V(log.DEBUG).Infof("Install/ensure %s chain exists", constants.SmPostRoutingChain)

	if err = util.CreateChainIfNotExists(ipt, "nat", constants.SmPostRoutingChain); err != nil {
		return fmt.Errorf("unable to create %s chain in iptables: %v", constants.SmPostRoutingChain, err)
	}

	klog.V(log.DEBUG).Infof("Insert %s rule that has rules for inter-cluster traffic", constants.SmPostRoutingChain)
	forwardToSubPostroutingRuleSpec := []string{"-j", constants.SmPostRoutingChain}
	if err = util.PrependUnique(ipt, "nat", "POSTROUTING", forwardToSubPostroutingRuleSpec); err != nil {
		return fmt.Errorf("unable to insert iptable rule in NAT table, POSTROUTING chain: %v", err)
	}

	klog.V(log.DEBUG).Infof("Install/ensure SUBMARINER-INPUT chain exists")

	if err = util.CreateChainIfNotExists(ipt, "filter", "SUBMARINER-INPUT"); err != nil {
		return fmt.Errorf("unable to create SUBMARINER-INPUT chain in iptables: %v", err)
	}

	forwardToSubInputRuleSpec := []string{"-p", "udp", "-m", "udp", "-j", "SUBMARINER-INPUT"}
	if err = ipt.AppendUnique("filter", "INPUT", forwardToSubInputRuleSpec...); err != nil {
		return fmt.Errorf("unable to append iptables rule %q: %v\n", strings.Join(forwardToSubInputRuleSpec, " "), err)
	}

	klog.V(log.DEBUG).Infof("Allow VxLAN incoming traffic in SUBMARINER-INPUT Chain")

	ruleSpec := []string{"-p", "udp", "-m", "udp", "--dport", strconv.Itoa(VxLANPort), "-j", "ACCEPT"}

	if err = ipt.AppendUnique("filter", "SUBMARINER-INPUT", ruleSpec...); err != nil {
		return fmt.Errorf("unable to append iptables rule %q: %v\n", strings.Join(ruleSpec, " "), err)
	}

	klog.V(log.DEBUG).Infof("Insert rule to allow traffic over %s interface in FORWARDing Chain", VxLANIface)

	ruleSpec = []string{"-o", VxLANIface, "-j", "ACCEPT"}

	if err = util.PrependUnique(ipt, "filter", "FORWARD", ruleSpec); err != nil {
		return fmt.Errorf("unable to insert iptable rule in filter table to allow vxlan traffic: %v", err)
	}

	if kp.cniIface != nil {
		// Program rules to support communication from HostNetwork to remoteCluster
		sourceAddress := strconv.Itoa(VxLANVTepNetworkPrefix) + ".0.0.0/8"
		ruleSpec = []string{"-s", sourceAddress, "-o", VxLANIface, "-j", "SNAT", "--to", kp.cniIface.IPAddress}
		klog.V(log.DEBUG).Infof("Installing rule for host network to remote cluster communication: %s", strings.Join(ruleSpec, " "))

		if err = ipt.AppendUnique("nat", constants.SmPostRoutingChain, ruleSpec...); err != nil {
			return fmt.Errorf("error appending iptables rule %q: %v\n", strings.Join(ruleSpec, " "), err)
		}
	}

	return nil
}

func (kp *SyncHandler) updateIptableRulesForInterclusterTraffic(inputCidrBlocks []string) {
	for _, inputCidrBlock := range inputCidrBlocks {
		if !kp.remoteSubnets.Contains(inputCidrBlock) {
			kp.remoteSubnets.Add(inputCidrBlock)
			err := kp.programIptableRulesForInterClusterTraffic(inputCidrBlock)
			if err != nil {
				klog.Errorf("Failed to program iptable rule. %v", err)
			}
		}
	}
}

func (kp *SyncHandler) programIptableRulesForInterClusterTraffic(remoteCidrBlock string) error {
	ipt, err := iptables.New()
	if err != nil {
		return fmt.Errorf("error initializing iptables: %v", err)
	}

	for _, localClusterCidr := range kp.localClusterCidr {
		ruleSpec := []string{"-s", localClusterCidr, "-d", remoteCidrBlock, "-j", "ACCEPT"}
		klog.V(log.DEBUG).Infof("Installing iptables rule for outgoing traffic: %s", strings.Join(ruleSpec, " "))

		if err = ipt.AppendUnique("nat", constants.SmPostRoutingChain, ruleSpec...); err != nil {
			return fmt.Errorf("error appending iptables rule \"%s\": %v\n", strings.Join(ruleSpec, " "), err)
		}

		// TODO: revisit, we only have to program rules to allow traffic from the podCidr
		ruleSpec = []string{"-s", remoteCidrBlock, "-d", localClusterCidr, "-j", "ACCEPT"}
		klog.V(log.DEBUG).Infof("Installing iptables rule for incoming traffic: %s", strings.Join(ruleSpec, " "))

		if err = ipt.AppendUnique("nat", constants.SmPostRoutingChain, ruleSpec...); err != nil {
			return fmt.Errorf("error appending iptables rule \"%s\": %v\n", strings.Join(ruleSpec, " "), err)
		}
	}

	return nil
}