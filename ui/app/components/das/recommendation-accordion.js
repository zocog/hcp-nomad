import Component from '@glimmer/component';
import { action } from '@ember/object';
import { tracked } from '@glimmer/tracking';
import { task, timeout } from 'ember-concurrency';
import ResourcesDiffs from 'nomad-ui/utils/resources-diffs';

export default class DasRecommendationAccordionComponent extends Component {
  @tracked waitingToProceed = false;

  @(task(function*() {
    this.waitingToProceed = false;
    yield timeout(0);
  }).drop())
  proceed;

  @action
  inserted() {
    this.waitingToProceed = true;
  }

  get show() {
    return !this.args.summary.isProcessed || this.waitingToProceed;
  }

  get diffs() {
    const summary = this.args.summary;
    const taskGroup = summary.taskGroup;

    return new ResourcesDiffs(
      taskGroup,
      taskGroup.count,
      this.args.summary.recommendations,
      this.args.summary.excludedRecommendations
    );
  }
}
