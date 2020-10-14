import Component from '@glimmer/component';
import { tracked } from '@glimmer/tracking';
import { task, timeout } from 'ember-concurrency';
import ResourcesDiffs from 'nomad-ui/utils/resources-diffs';

export default class DasRecommendationAccordionComponent extends Component {
  @tracked processed = false;

  @(task(function*() {
    this.processed = true;
    yield timeout(0);
  }).drop())
  proceed;

  get diffs() {
    const summary = this.args.summary;
    const taskGroup = summary.taskGroup;

    return new ResourcesDiffs(
      taskGroup,
      taskGroup.allocations.length,
      this.args.summary.recommendations,
      this.args.summary.excludedRecommendations
    );
  }
}