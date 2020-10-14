import Component from '@glimmer/component';
import { tracked } from '@glimmer/tracking';
import { task, timeout } from 'ember-concurrency';

export default class DasRecommendationAccordionComponent extends Component {
  @tracked processed = false;

  @(task(function*() {
    this.processed = true;
    yield timeout(0);
  }).drop())
  proceed;
}